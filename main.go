package main

import (
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/go-redis/redis"
	"strings"
)

const (
	CONN_HOST  = "0.0.0.0"
	CONN_PORT  = "3333"
	CONN_TYPE  = "tcp"
	REDIS_ADDR = "pastey-redis:6379"
	REDIS_PASS = ""
	REDIS_DB   = 0
)

func main() {
	l, err := net.Listen(CONN_TYPE, CONN_HOST+":"+CONN_PORT)
	if err != nil {
		fmt.Println("Error listening:", err.Error())
		os.Exit(1)
	}
	defer l.Close()
	fmt.Println("Listening on " + CONN_HOST + ":" + CONN_PORT)

	client := redis.NewClient(&redis.Options{
		Addr:     REDIS_ADDR,
		Password: REDIS_PASS,
		DB:       REDIS_DB,
	})

	_, err = client.Ping().Result()
	if err != nil {
		fmt.Println("could not connect to redis")
		os.Exit(1)
	}
	fmt.Println("Connected to redis")

	for {
		conn, err := l.Accept()
		if err != nil {
			fmt.Println("Error accepting: ", err.Error())
			os.Exit(1)
		}
		go handleRequest(conn, client)
	}
}

func handleRequest(conn net.Conn, redisClient *redis.Client) {
	msg := make([]byte, 0)
	buf := make([]byte, 1024)
	bytesRead := 0

	conn.SetReadDeadline(time.Now().Add(time.Second * 5))

	for {
		n, err := conn.Read(buf)
		if err != nil {
			if err != io.EOF && !strings.Contains(err.Error(), "i/o timeout") {
				fmt.Println("read error:", err)
				conn.Write([]byte("read err\r\n"))
				conn.Close()
				return
			}
			break
		}

		bytesRead += n

		if bytesRead > 250000 {
			fmt.Println("i dont want your harddrive")
			conn.Write([]byte("too much data\r\n"))
			conn.Close()

			break
		}

		msg = append(msg, buf[:n]...)

		conn.SetReadDeadline(time.Now().Add(time.Second * 5))

	}

	identifier := ""
	tried := 0
	for {
		identifier = randString(4)
		val, err := redisClient.Get("pastey_" + identifier).Result()

		if err != nil {
			if err == redis.Nil {
				// value no existo
				break
			} else if err != nil {
				// ya fucked
				fmt.Println(err.Error())
				conn.Write([]byte("error\r\n"))
				conn.Close()
				return
			}
		}

		if val == "" {
			break
		}

		identifier = ""

		tried++
		fmt.Println("identifier mismatch")

		if tried > 5 {
			//we cant be that unlucky, fail
			break
		}
	}

	if identifier == "" {
		// ya fucked again
		fmt.Println("identifier could not be genned")
		conn.Write([]byte("error\r\n"))
		conn.Close()
		return
	}

	// got identifier

	err := redisClient.Set("pastey_"+identifier, string(msg), time.Hour*72)

	if err.Err() != nil {
		conn.Write([]byte("error, could not connect to db\r\n"))
		conn.Close()
		return
	}

	fmt.Println("made new paste " + identifier + " for " + conn.RemoteAddr().String())

	conn.Write([]byte("https://bind.sh/" + identifier + "\r\n"))
	conn.Close()
}

func randString(n int) string {
	const alphanum = "123456789abcdefghijklmnopqrstuvwxyz"
	var bytes = make([]byte, n)
	rand.Read(bytes)
	for i, b := range bytes {
		bytes[i] = alphanum[b%byte(len(alphanum))]
	}
	return string(bytes)
}
