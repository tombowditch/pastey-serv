// Package client provides a Go client for the Pastey paste service (https://ig.lc).
//
// # Installation
//
//	go get github.com/tombowditch/pastey-serv/client
//
// # Quick Start
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//		"log"
//
//		"github.com/tombowditch/pastey-serv/client"
//	)
//
//	func main() {
//		c := client.New()
//
//		// Create a paste
//		url, err := c.Create(context.Background(), []byte("Hello, World!"))
//		if err != nil {
//			log.Fatal(err)
//		}
//		fmt.Println("Paste URL:", url)
//
//		// Retrieve a paste (by URL or ID)
//		content, err := c.Get(context.Background(), url)
//		if err != nil {
//			log.Fatal(err)
//		}
//		fmt.Println("Content:", string(content))
//	}
//
// # Secure Pastes
//
// For longer, harder-to-guess paste IDs (32 chars instead of 7):
//
//	url, err := c.CreateWithOptions(ctx, content, client.CreateOptions{Secure: true})
//
// # Custom Configuration
//
//	c := client.New(
//		client.WithBaseURL("https://your-pastey-instance.com"),
//		client.WithTimeout(10 * time.Second),
//	)
//
// # Error Handling
//
//	content, err := c.Get(ctx, "abc123")
//	if client.IsNotFound(err) {
//		// Paste expired or doesn't exist
//	}
//	if client.IsRateLimited(err) {
//		// Too many requests, back off
//	}
package client
