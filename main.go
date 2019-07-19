package main

import (
	"flag"
	"fmt"
	"net"
)

var port = flag.String("port", ":9999", "Port rocky clients connect to")
var clientPort = flag.String("client-port", ":5000", "Port real clients connect to")

func main() {
	flag.Parse()
	ln, err := net.Listen("tcp", *port)
	if err != nil {
		fmt.Println(err)
		return
	}

	ln2, err := net.Listen("tcp", *clientPort)
	if err != nil {
		fmt.Println(err)
		return
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			// handle error
		}
		go handleIncomingClients(conn, ln2)
	}
}

func handleToTarget(conn net.Conn, targetConn net.Conn) {
	for {
		buf := make([]byte, 1)
		_, err := conn.Read(buf)
		if err != nil {
			fmt.Println("Error reading to target:", err.Error())
			return
		}

		targetConn.Write(buf)
	}
}

func handleFromTarget(conn net.Conn, targetConn net.Conn) {
	for {
		buf := make([]byte, 1)
		_, err := targetConn.Read(buf)
		if err != nil {
			fmt.Println("Error reading from target:", err.Error())
			return
		}
		conn.Write(buf)
	}
}

func handleIncomingClients(targetConn net.Conn, ln net.Listener) {
	fmt.Println("Handling new clients.")
	for {
		clientConn, err := ln.Accept()
		if err != nil {
			// handle error
		}
		fmt.Println("New connection")
		go handleToTarget(clientConn, targetConn)
		go handleFromTarget(clientConn, targetConn)
	}
}
