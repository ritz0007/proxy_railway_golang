package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

var (
	activeConnections int64
	totalRequests     int64
	instanceID        string
)

func init() {
	// Generate unique instance ID for multi-instance tracking
	hostname, _ := os.Hostname()
	instanceID = fmt.Sprintf("%s-%d", hostname, time.Now().UnixNano()%10000)
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Optional authentication
	proxyUser := os.Getenv("PROXY_USER")
	proxyPass := os.Getenv("PROXY_PASS")

	proxy := &ProxyServer{
		username: proxyUser,
		password: proxyPass,
	}

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      proxy,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	log.Printf("🚀 Proxy server [Instance: %s] starting on port %s", instanceID, port)
	if proxyUser != "" {
		log.Println("🔐 Authentication enabled")
	} else {
		log.Println("⚠️  Authentication disabled (set PROXY_USER and PROXY_PASS to enable)")
	}

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

type ProxyServer struct {
	username string
	password string
}

func (p *ProxyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt64(&totalRequests, 1)

	// Health check endpoint for Railway
	if r.URL.Path == "/health" || r.URL.Path == "/_health" {
		p.handleHealth(w, r)
		return
	}

	// Stats endpoint
	if r.URL.Path == "/stats" || r.URL.Path == "/_stats" {
		p.handleStats(w, r)
		return
	}

	// Authentication check
	if !p.authenticate(r) {
		w.Header().Set("Proxy-Authenticate", `Basic realm="Proxy"`)
		http.Error(w, "Proxy Authentication Required", http.StatusProxyAuthRequired)
		return
	}

	// Handle CONNECT method (HTTPS tunneling)
	if r.Method == http.MethodConnect {
		p.handleConnect(w, r)
		return
	}

	// Handle regular HTTP requests
	p.handleHTTP(w, r)
}

func (p *ProxyServer) authenticate(r *http.Request) bool {
	if p.username == "" && p.password == "" {
		return true
	}

	auth := r.Header.Get("Proxy-Authorization")
	if auth == "" {
		return false
	}

	username, password, ok := parseBasicAuth(auth)
	if !ok {
		return false
	}

	return username == p.username && password == p.password
}

func parseBasicAuth(auth string) (username, password string, ok bool) {
	const prefix = "Basic "
	if !strings.HasPrefix(auth, prefix) {
		return "", "", false
	}

	decoded, err := base64Decode(strings.TrimPrefix(auth, prefix))
	if err != nil {
		return "", "", false
	}

	parts := strings.SplitN(decoded, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}

	return parts[0], parts[1], true
}

func base64Decode(s string) (string, error) {
	// Simple base64 decode
	const base64Chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	
	// Remove padding
	s = strings.TrimRight(s, "=")
	
	var result []byte
	var buffer int
	var bitsCollected int
	
	for _, c := range s {
		idx := strings.IndexRune(base64Chars, c)
		if idx == -1 {
			continue
		}
		buffer = (buffer << 6) | idx
		bitsCollected += 6
		if bitsCollected >= 8 {
			bitsCollected -= 8
			result = append(result, byte(buffer>>bitsCollected))
			buffer &= (1 << bitsCollected) - 1
		}
	}
	
	return string(result), nil
}

func (p *ProxyServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"healthy","instance":"%s","connections":%d,"requests":%d}`,
		instanceID, atomic.LoadInt64(&activeConnections), atomic.LoadInt64(&totalRequests))
}

func (p *ProxyServer) handleStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{
		"instance_id": "%s",
		"active_connections": %d,
		"total_requests": %d,
		"uptime": "%s"
	}`, instanceID, atomic.LoadInt64(&activeConnections), atomic.LoadInt64(&totalRequests), time.Since(startTime).String())
}

var startTime = time.Now()

func (p *ProxyServer) handleConnect(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt64(&activeConnections, 1)
	defer atomic.AddInt64(&activeConnections, -1)

	log.Printf("[%s] CONNECT %s", instanceID, r.Host)

	// Connect to the target server
	targetConn, err := net.DialTimeout("tcp", r.Host, 30*time.Second)
	if err != nil {
		log.Printf("[%s] Failed to connect to %s: %v", instanceID, r.Host, err)
		http.Error(w, "Failed to connect to target", http.StatusBadGateway)
		return
	}
	defer targetConn.Close()

	// Hijack the connection
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer clientConn.Close()

	// Send 200 Connection Established
	clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	// Bidirectional copy
	done := make(chan bool, 2)

	go func() {
		io.Copy(targetConn, clientConn)
		done <- true
	}()

	go func() {
		io.Copy(clientConn, targetConn)
		done <- true
	}()

	<-done
}

func (p *ProxyServer) handleHTTP(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt64(&activeConnections, 1)
	defer atomic.AddInt64(&activeConnections, -1)

	log.Printf("[%s] %s %s", instanceID, r.Method, r.URL.String())

	// Create the proxy request
	targetURL := r.URL
	if !targetURL.IsAbs() {
		http.Error(w, "Invalid URL", http.StatusBadRequest)
		return
	}

	// Create transport with custom settings
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: false,
		},
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   60 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Create new request
	proxyReq, err := http.NewRequest(r.Method, targetURL.String(), r.Body)
	if err != nil {
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}

	// Copy headers
	copyHeaders(proxyReq.Header, r.Header)
	
	// Remove proxy-specific headers
	proxyReq.Header.Del("Proxy-Authorization")
	proxyReq.Header.Del("Proxy-Connection")

	// Add forwarding headers
	if clientIP, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		if prior := proxyReq.Header.Get("X-Forwarded-For"); prior != "" {
			proxyReq.Header.Set("X-Forwarded-For", prior+", "+clientIP)
		} else {
			proxyReq.Header.Set("X-Forwarded-For", clientIP)
		}
	}
	proxyReq.Header.Set("X-Proxy-Instance", instanceID)

	// Execute request
	resp, err := client.Do(proxyReq)
	if err != nil {
		log.Printf("[%s] Request failed: %v", instanceID, err)
		http.Error(w, "Request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	copyHeaders(w.Header(), resp.Header)
	w.Header().Set("X-Proxy-Instance", instanceID)
	
	// Write status code
	w.WriteHeader(resp.StatusCode)

	// Copy response body
	io.Copy(w, resp.Body)
}

func copyHeaders(dst, src http.Header) {
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}
