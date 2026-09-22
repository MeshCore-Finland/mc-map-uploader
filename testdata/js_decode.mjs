import { pathToFileURL } from 'node:url';

const [modulePath, rawHex] = process.argv.slice(2);
const { Packet, Advert, BufferUtils } = await import(pathToFileURL(modulePath).href);
const packet = Packet.fromBytes(Buffer.from(rawHex, 'hex'));
const advert = Advert.fromBytes(packet.payload);
console.log(JSON.stringify({
  publicKey: BufferUtils.bytesToHex(advert.publicKey).toLowerCase(),
  timestamp: advert.timestamp,
  type: advert.parsed.type,
  name: advert.parsed.name,
  verified: await advert.isVerified(),
}));
