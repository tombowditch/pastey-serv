package main

import (
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-redis/redis"
	"github.com/julienschmidt/httprouter"
	"github.com/sirupsen/logrus"
	rate "github.com/wallstreetcn/rate/redis"
)

const (
	CONN_HOST  = "0.0.0.0"
	CONN_PORT  = "9999"
	CONN_TYPE  = "tcp"
	REDIS_PASS = ""
	REDIS_DB   = 0
)

var BLACKLISTED_PHRASES = [...]string{"Cookie: mstshash=Administ", "-esystem('cmd /c echo .close", "md /c echo Set xHttp=createobjec"}

var client *redis.Client

func main() {
	client = redis.NewClient(&redis.Options{
		Addr:     os.Getenv("REDIS_URI"),
		Password: REDIS_PASS,
		DB:       REDIS_DB,
	})

	rate.SetRedis(&rate.ConfigRedis{
		Host: os.Getenv("REDIS_URI"),
		Port: 6379,
		Auth: "",
	})

	_, err := client.Ping().Result()
	if err != nil {
		fmt.Println("could not connect to redis")
		os.Exit(1)
	}
	fmt.Println("Connected to redis")

	// Start TCP server
	go startTCPServer()

	// Start HTTP server
	logrus.Info("starting http server...")

	r := httprouter.New()

	r.GET("/", indexPage)
	r.GET("/:identifier", getIdentifier)
	r.POST("/create", createPaste)

	if err := http.ListenAndServe("0.0.0.0:3334", r); err != nil {
		logrus.Error(err.Error())
		os.Exit(1)
	}
}

func startTCPServer() {
	l, err := net.Listen(CONN_TYPE, CONN_HOST+":"+CONN_PORT)
	if err != nil {
		fmt.Println("Error listening:", err.Error())
		os.Exit(1)
	}
	defer l.Close()
	fmt.Println("Listening on " + CONN_HOST + ":" + CONN_PORT)

	for {
		conn, err := l.Accept()
		if err != nil {
			fmt.Println("Error accepting: ", err.Error())
			continue
		}
		go handleRequest(conn)
	}
}

func indexPage(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(`ig.lc - commandline pastebin

pipe to 'nc ig.lc 9999'

- pastes are stored for 72 hours, after which they are automatically deleted

example
=======

~> echo "hello" | nc ig.lc 9999
https://ig.lc/yourpaste

~> cat /etc/nginx/nginx.conf | nc ig.lc 9999
https://ig.lc/yourpaste

~> cat 100mb.bin | nc ig.lc 9999
too much data`))
}

func getIdentifier(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	identifier := ps.ByName("identifier")

	val, _ := client.Get("pastey_" + identifier).Result()

	if val != "" {
		// yea
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(val))
	} else {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found or expired"))
	}
}

func createPaste(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	// Rate limit: 1 paste per 5 seconds per IP
	cip, _, _ := net.SplitHostPort(r.RemoteAddr)
	limiter := rate.NewLimiter(rate.Every(time.Second*5), 1, "pastey_http_create_rl_"+cip)
	if !limiter.Allow() {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("rate limit exceeded (1 paste per 5 seconds)"))
		return
	}

	// Read body (max 5MB)
	body, err := io.ReadAll(io.LimitReader(r.Body, 5000001))
	if err != nil {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("error reading body"))
		return
	}
	defer r.Body.Close()

	if len(body) > 5000000 {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		w.Write([]byte("payload too big"))
		return
	}

	if len(body) == 0 {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("empty body"))
		return
	}

	// Check blacklist
	for _, phrase := range BLACKLISTED_PHRASES {
		if strings.Contains(string(body), phrase) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("blacklisted phrases, antispam system\ncontact admin@ig.lc if this is in error"))
			return
		}
	}

	// Determine identifier length: 32 chars if secure=true, otherwise 4
	idLength := 4
	if r.URL.Query().Get("secure") == "true" {
		idLength = 32
	}

	// Generate unique identifier
	identifier := ""
	tried := 0
	for {
		identifier = randString(idLength)
		val, err := client.Get("pastey_" + identifier).Result()
		if err != nil {
			if err == redis.Nil {
				break
			}
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("error"))
			return
		}
		if val == "" {
			break
		}
		identifier = ""
		tried++
		if tried > 5 {
			break
		}
	}

	if identifier == "" {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("could not generate identifier"))
		return
	}

	// Store paste
	result := client.Set("pastey_"+identifier, string(body), time.Hour*72)
	if result.Err() != nil {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("error, could not connect to db"))
		return
	}

	fmt.Println("made new paste " + identifier + " via HTTP POST from " + r.RemoteAddr)

	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusCreated)
	w.Write([]byte("https://ig.lc/" + identifier + "\n"))
}

func handleRequest(conn net.Conn) {
	msg := make([]byte, 0)
	buf := make([]byte, 1024)
	bytesRead := 0

	// before we even try to read, are we ratelimited?
	cip := strings.Split(conn.RemoteAddr().String(), ":")[0]
	limiter := rate.NewLimiter(rate.Every(time.Second*5), 5, "pastey_rl_"+cip)
	if !limiter.Allow() {
		fmt.Println("rate limit exceeded for " + cip)
		conn.Write([]byte("rate limit exceeded (1 paste per 5 seconds)\r\n"))
		conn.Close()
		return
	}

	conn.SetReadDeadline(time.Now().Add(time.Second * 5))

	for {
		n, err := conn.Read(buf)
		if err != nil {
			if netErr, _ := err.(net.Error); err != io.EOF && !netErr.Timeout() {
				fmt.Println("read error:", err)
				conn.Write([]byte("read err\r\n"))
				conn.Close()
				return
			}
			break
		}

		bytesRead += n

		if bytesRead > 5000000 {
			conn.Write([]byte("payload too big\r\n"))
			conn.Close()

			return
		}

		msg = append(msg, buf[:n]...)

		conn.SetReadDeadline(time.Now().Add(time.Second * 2))
	}

	identifier := ""
	tried := 0
	for {
		identifier = randString(4)
		val, err := client.Get("pastey_" + identifier).Result()
		if err != nil {
			if err == redis.Nil {
				// value doesn't exist
				break
			} else if err != nil {
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
			// we cant be that unlucky, fail
			break
		}
	}

	if identifier == "" {
		fmt.Println("identifier could not be genned") // you should never realistically run into this
		conn.Write([]byte("error\r\n"))
		conn.Close()
		return
	}

	// got identifier

	// check if bad
	blacklisted := false
	for _, phrase := range BLACKLISTED_PHRASES {
		if strings.Contains(string(msg), phrase) {
			blacklisted = true
		}
	}

	if blacklisted {
		conn.Write([]byte("blacklisted phrases, antispam system\r\ncontact admin@ig.lc if this is in error\r\n"))
		conn.Close()
		return
	}

	err := client.Set("pastey_"+identifier, string(msg), time.Hour*72)

	if err.Err() != nil {
		conn.Write([]byte("error, could not connect to db\r\n"))
		conn.Close()
		return
	}

	fmt.Println("made new paste " + identifier + " for " + conn.RemoteAddr().String())

	conn.Write([]byte("https://ig.lc/" + identifier + "\r\n"))
	conn.Close()
}

func randString(n int) string {
	const alphanum = "123456789abcdefghijklmnopqrstuvwxyz"
	bytes := make([]byte, n)
	rand.Read(bytes)
	for i, b := range bytes {
		bytes[i] = alphanum[b%byte(len(alphanum))]
	}
	return string(bytes)
}
