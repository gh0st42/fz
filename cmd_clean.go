package main

import (
	"fmt"
	"os"
)

func runClean() error {
	if err := os.RemoveAll("dist"); err != nil {
		return err
	}

	fmt.Println("Removed dist directory")
	return nil
}
