package engine

import "encoding/gob"

func init() {
	gob.Register(map[string]string{})
	gob.Register([]string{})
	gob.Register(map[string]struct{}{})
	gob.Register(map[string]float64{})
}
