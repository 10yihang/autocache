package main

import (
	"fmt"
	"github.com/10yihang/autocache/internal/cluster/hash"
)

func main() {
	targetSlot := uint16(0)
	for i := 0; i < 100000; i++ {
		key := fmt.Sprintf("key-%d", i)
		if hash.KeySlot(key) == targetSlot {
			fmt.Println(key)
			return
		}
	}
	fmt.Println("Not found")
}
