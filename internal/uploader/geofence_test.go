package uploader

import "testing"

func TestGeofencePolygons(t *testing.T) {
	fence, err := newGeofence([]Polygon{
		{{60, 24}, {60, 25}, {61, 25}, {61, 24}},
		{{65, 20}, {65, 21}, {66, 21}, {66, 20}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, point := range [][2]float64{{60.5, 24.5}, {65.5, 20.5}, {60, 24.5}} {
		if !fence.contains(point[0], point[1]) {
			t.Fatalf("inside or boundary point %v rejected", point)
		}
	}
	for _, point := range [][2]float64{{59.5, 24.5}, {60.5, 25.5}, {64, 20.5}} {
		if fence.contains(point[0], point[1]) {
			t.Fatalf("outside point %v accepted", point)
		}
	}
	if _, err := newGeofence([]Polygon{{{60, 24}, {60, 25}}}); err == nil {
		t.Fatal("two-point polygon accepted")
	}
}
