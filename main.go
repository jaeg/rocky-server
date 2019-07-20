package main

import (
	"flag"
	"fmt"
	"net"
)

var port = flag.String("port", ":9999", "Port rocky clients connect to")
var clientPort = flag.String("client-port", ":5000", "Port real clients connect to")
var connections map[string]net.Conn

func main() {
	connections = make(map[string]net.Conn)
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

	proxyLn, err := net.Listen("tcp", ":9998")
	if err != nil {
		fmt.Println(err)
		return
	}
	go handleProxy(proxyLn)

	for {
		conn, err := ln.Accept()
		if err != nil {
			// handle error
		}
		go handleIncomingClients(conn, ln2)
	}
}

func handleProxy(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			fmt.Println("Proxy thread failed")
			return
		}
		buf := make([]byte, 10)
		conn.Read(buf)
		id := string(buf)
		connections[id] = conn
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
	defer fmt.Println("Incoming Thread Died")
	for {
		clientConn, err := ln.Accept()
		if err != nil {
			return
		}
		fmt.Println("New connection")

		targetConn.Write([]byte("New\n"))
		targetConn.Write([]byte("1\n"))
		fmt.Println("SEnd id")
		buf := make([]byte, 10)
		targetConn.Read(buf)
		id := string(buf)
		fmt.Println("ID", id)
		for connections[id] == nil {
			//fmt.Println(connections)
		}

		go handleToTarget(clientConn, connections[id])
		go handleFromTarget(clientConn, connections[id])
	}
}
