package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
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
	mtprotoSecret     string
)

func init() {
	// Generate unique instance ID for multi-instance tracking
	hostname, _ := os.Hostname()
	instanceID = fmt.Sprintf("%s-%d", hostname, time.Now().UnixNano()%10000)
	
	// Generate unique MTProto secret for this instance
	mtprotoSecret = generateMTProtoSecret()
}

// generateMTProtoSecret generates a random 32-byte (64 hex char) secret for MTProto
func generateMTProtoSecret() string {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		log.Fatalf("Failed to generate MTProto secret: %v", err)
	}
	return hex.EncodeToString(secret)
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

	// Print startup information
	log.Printf("🚀 Proxy server [Instance: %s] starting on port %s", instanceID, port)
	if proxyUser != "" {
		log.Println("🔐 Authentication enabled")
	} else {
		log.Println("⚠️  Authentication disabled (set PROXY_USER and PROXY_PASS to enable)")
	}
	
	// Print MTProto connection information
	printMTProtoConnectionInfo(port)

	// Create a custom listener that can handle both HTTP and MTProto
	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("Failed to start listener: %v", err)
	}
	defer listener.Close()

	log.Printf("✅ Server ready and listening on port %s", port)

	// Accept connections and route based on protocol
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept connection: %v", err)
			continue
		}
		
		go handleConnection(conn, proxy)
	}
}

// handleConnection detects the protocol and routes to appropriate handler
func handleConnection(conn net.Conn, proxy *ProxyServer) {
	// Don't defer close here - let the handlers close it
	
	// Peek at the first few bytes to detect protocol
	// HTTP starts with method name (GET, POST, CONNECT, etc.)
	// MTProto has a different handshake pattern
	buf := make([]byte, 8)
	n, err := conn.Read(buf)
	if err != nil {
		conn.Close()
		return
	}
	
	// Check if it looks like HTTP
	isHTTP := false
	httpMethods := []string{"GET ", "POST", "PUT ", "DELE", "HEAD", "OPTI", "PATC", "CONN", "TRAC"}
	bufStr := string(buf[:n])
	for _, method := range httpMethods {
		if strings.HasPrefix(bufStr, method) {
			isHTTP = true
			break
		}
	}
	
	if isHTTP {
		// Handle as HTTP - we need to wrap the connection with the already-read bytes
		handleHTTPConnection(conn, buf[:n], proxy)
	} else {
		// Handle as MTProto
		handleMTProtoConnectionWithBuffer(conn, buf[:n])
	}
}

// handleHTTPConnection handles HTTP connections through the proxy server
func handleHTTPConnection(conn net.Conn, initialData []byte, proxy *ProxyServer) {
	defer conn.Close()
	
	// Create a buffer that prepends the initial data
	reader := io.MultiReader(bytes.NewReader(initialData), conn)
	bufReader := bufio.NewReader(reader)
	
	// Read the HTTP request
	req, err := http.ReadRequest(bufReader)
	if err != nil {
		log.Printf("Failed to read HTTP request: %v", err)
		return
	}
	
	// Create a response writer that writes to the connection
	w := &connResponseWriter{
		conn:   conn,
		header: make(http.Header),
	}
	
	// Handle the request using our proxy handler
	proxy.ServeHTTP(w, req)
}

// connResponseWriter implements http.ResponseWriter for raw connections
type connResponseWriter struct {
	conn          net.Conn
	header        http.Header
	statusCode    int
	wroteHeader   bool
}

func (w *connResponseWriter) Header() http.Header {
	return w.header
}

func (w *connResponseWriter) WriteHeader(statusCode int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.statusCode = statusCode
	
	// Write status line
	fmt.Fprintf(w.conn, "HTTP/1.1 %d %s\r\n", statusCode, http.StatusText(statusCode))
	
	// Write headers
	w.header.Write(w.conn)
	
	// Write empty line to end headers
	fmt.Fprintf(w.conn, "\r\n")
}

func (w *connResponseWriter) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.conn.Write(data)
}

// printMTProtoConnectionInfo prints the Telegram connection details
func printMTProtoConnectionInfo(port string) {
	// Get Railway public domain
	domain := os.Getenv("RAILWAY_PUBLIC_DOMAIN")
	if domain == "" {
		domain = os.Getenv("RAILWAY_STATIC_URL")
		// Strip protocol if present (RAILWAY_STATIC_URL may include https://)
		domain = strings.TrimPrefix(domain, "https://")
		domain = strings.TrimPrefix(domain, "http://")
	}
	
	log.Println("\n" + strings.Repeat("=", 80))
	log.Printf("🔐 MTProto Secret: %s", mtprotoSecret)
	
	if domain != "" {
		// Railway exposes services on port 443 externally via HTTPS
		telegramURL := fmt.Sprintf("https://t.me/proxy?server=%s&port=443&secret=%s", domain, mtprotoSecret)
		log.Printf("📱 Telegram Connection URL: %s", telegramURL)
		log.Println("\n✅ Server ready! Connect your Telegram client using the URL above.")
	} else {
		log.Println("⚠️  Railway domain not detected. Set RAILWAY_PUBLIC_DOMAIN or RAILWAY_STATIC_URL")
		log.Printf("📱 Telegram Connection Format: https://t.me/proxy?server=<your-domain>&port=443&secret=%s", mtprotoSecret)
	}
	log.Println(strings.Repeat("=", 80) + "\n")
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

// MTProto proxy implementation
// Telegram datacenters - these are the servers MTProto proxy connects to
var telegramDCs = []string{
	"149.154.175.50:443",
	"149.154.167.51:443",
	"149.154.175.100:443",
	"149.154.167.91:443",
	"149.154.171.5:443",
}

// obfuscateData applies XOR obfuscation to data using the secret key
func obfuscateData(data []byte, secretBytes []byte) {
	for i := 0; i < len(data) && i < len(secretBytes); i++ {
		data[i] ^= secretBytes[i%len(secretBytes)]
	}
}

// handleMTProtoConnectionWithBuffer handles MTProto connection with initial buffered data
func handleMTProtoConnectionWithBuffer(clientConn net.Conn, initialData []byte) {
	defer clientConn.Close()
	
	atomic.AddInt64(&activeConnections, 1)
	defer atomic.AddInt64(&activeConnections, -1)
	
	// Read the rest of the handshake (first 64 bytes contain protocol info)
	handshake := make([]byte, 64)
	copy(handshake, initialData)
	
	remaining := 64 - len(initialData)
	if remaining > 0 {
		n, err := io.ReadFull(clientConn, handshake[len(initialData):])
		if err != nil || n != remaining {
			log.Printf("[MTProto] Failed to read handshake: %v", err)
			return
		}
	}
	
	// Decode the handshake with our secret
	secretBytes, err := hex.DecodeString(mtprotoSecret)
	if err != nil {
		log.Printf("[MTProto] Failed to decode secret: %v", err)
		return
	}
	
	// Apply obfuscation to handshake
	obfuscateData(handshake, secretBytes)
	
	// Try to connect to Telegram DC (use the first one as default)
	dcConn, err := net.DialTimeout("tcp", telegramDCs[0], 30*time.Second)
	if err != nil {
		log.Printf("[MTProto] Failed to connect to Telegram DC: %v", err)
		return
	}
	defer dcConn.Close()
	
	// Re-encode handshake for Telegram DC
	obfuscateData(handshake, secretBytes)
	
	// Send handshake to Telegram
	if _, err := dcConn.Write(handshake); err != nil {
		log.Printf("[MTProto] Failed to send handshake to DC: %v", err)
		return
	}
	
	log.Printf("[MTProto] Connection established [Instance: %s]", instanceID)
	
	// Bidirectional copy with obfuscation
	done := make(chan bool, 2)
	
	// Client -> DC
	go func() {
		buffer := make([]byte, 32768)
		for {
			n, err := clientConn.Read(buffer)
			if err != nil {
				break
			}
			
			// Apply obfuscation
			data := make([]byte, n)
			copy(data, buffer[:n])
			obfuscateData(data, secretBytes)
			
			if _, err := dcConn.Write(data); err != nil {
				break
			}
		}
		done <- true
	}()
	
	// DC -> Client
	go func() {
		buffer := make([]byte, 32768)
		for {
			n, err := dcConn.Read(buffer)
			if err != nil {
				break
			}
			
			// Apply obfuscation
			data := make([]byte, n)
			copy(data, buffer[:n])
			obfuscateData(data, secretBytes)
			
			if _, err := clientConn.Write(data); err != nil {
				break
			}
		}
		done <- true
	}()
	
	<-done
	log.Printf("[MTProto] Connection closed [Instance: %s]", instanceID)
}
