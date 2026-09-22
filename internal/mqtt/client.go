package mqtt

import (
	"context"
	"errors"
	"log/slog"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

type Message struct {
	Topic   string
	Payload []byte
}

type Config struct {
	Address       string
	Username      string
	Password      string
	ClientID      string
	StatusFilter  string
	PacketsFilter string
}

// Run subscribes with QoS 0 and a clean MQTT 3.1.1 session. It reconnects
// after connection loss, dropping messages while disconnected.
func Run(ctx context.Context, cfg Config, handler func(Message), logger *slog.Logger) error {
	if cfg.Address == "" || cfg.ClientID == "" || cfg.StatusFilter == "" || cfg.PacketsFilter == "" || handler == nil {
		return errors.New("MQTT address, client ID, topic filters, and handler are required")
	}
	for ctx.Err() == nil {
		lost := make(chan error, 1)
		opts := paho.NewClientOptions().AddBroker("tcp://" + cfg.Address).
			SetClientID(cfg.ClientID).
			SetUsername(cfg.Username).
			SetPassword(cfg.Password).
			SetCleanSession(true).
			SetAutoReconnect(false).
			SetConnectTimeout(5 * time.Second).
			SetWriteTimeout(5 * time.Second).
			SetKeepAlive(30 * time.Second).
			SetOrderMatters(false).
			SetConnectionLostHandler(func(_ paho.Client, err error) {
				select {
				case lost <- err:
				default:
				}
			})
		client := paho.NewClient(opts)
		if token := client.Connect(); !token.WaitTimeout(6*time.Second) || token.Error() != nil {
			logger.Warn("MQTT connect failed", "error", token.Error())
			client.Disconnect(100)
		} else {
			callback := func(_ paho.Client, msg paho.Message) {
				handler(Message{Topic: msg.Topic(), Payload: append([]byte(nil), msg.Payload()...)})
			}
			filter := map[string]byte{
				cfg.StatusFilter:  0,
				cfg.PacketsFilter: 0,
			}
			token := client.SubscribeMultiple(filter, callback)
			if !token.WaitTimeout(6*time.Second) || token.Error() != nil {
				logger.Warn("MQTT subscribe failed", "error", token.Error())
				client.Disconnect(100)
			} else {
				logger.Info("subscribed to MQTT broker", "address", cfg.Address)
				select {
				case <-ctx.Done():
				case err := <-lost:
					logger.Warn("MQTT connection lost", "error", err)
				}
				client.Disconnect(100)
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
	return ctx.Err()
}
