package uploader

import (
	"errors"
	"math"
)

// A polygon is a sequence of [latitude, longitude] points. Multiple polygons
// form a union, so disconnected regions can share one geofence.
type Polygon [][2]float64

type geofence struct {
	polygons []Polygon
}

func newGeofence(polygons []Polygon) (geofence, error) {
	for _, polygon := range polygons {
		if len(polygon) < 3 {
			return geofence{}, errors.New("map.geofence_polygons must contain polygons with at least three points")
		}
		area := 0.0
		for _, point := range polygon {
			lat, lon := point[0], point[1]
			if math.IsNaN(lat) || math.IsInf(lat, 0) || math.IsNaN(lon) || math.IsInf(lon, 0) || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
				return geofence{}, errors.New("map.geofence_polygons contains an invalid latitude or longitude")
			}
		}
		for i, point := range polygon {
			next := polygon[(i+1)%len(polygon)]
			area += point[1]*next[0] - next[1]*point[0]
		}
		if math.Abs(area) < 1e-10 {
			return geofence{}, errors.New("map.geofence_polygons contains a polygon with no area")
		}
	}
	return geofence{polygons: polygons}, nil
}

// ValidateGeofence checks configured polygons without starting the uploader.
func ValidateGeofence(polygons []Polygon) error {
	_, err := newGeofence(polygons)
	return err
}

func (f geofence) contains(lat, lon float64) bool {
	if len(f.polygons) == 0 {
		return true
	}
	for _, polygon := range f.polygons {
		inside := false
		for i, j := 0, len(polygon)-1; i < len(polygon); j, i = i, i+1 {
			a, b := polygon[j], polygon[i]
			cross := (b[1]-a[1])*(lat-a[0]) - (b[0]-a[0])*(lon-a[1])
			if math.Abs(cross) < 1e-12 && lon >= math.Min(a[1], b[1]) && lon <= math.Max(a[1], b[1]) && lat >= math.Min(a[0], b[0]) && lat <= math.Max(a[0], b[0]) {
				return true
			}
			if (a[0] > lat) != (b[0] > lat) && lon < (b[1]-a[1])*(lat-a[0])/(b[0]-a[0])+a[1] {
				inside = !inside
			}
		}
		if inside {
			return true
		}
	}
	return false
}
