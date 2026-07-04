//go:build !windows

package main

import "fmt"

func main() {
	fmt.Println("tracklm-windows only runs on Windows")
}
