package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
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
	socks5Username    string
	socks5Password    string
)

func init() {
	// Generate unique instance ID for multi-instance tracking
	hostname, _ := os.Hostname()
	instanceID = fmt.Sprintf("%s-%d", hostname, time.Now().UnixNano()%10000)
}

// generateRandomString generates a random alphanumeric string of given length
func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		log.Fatalf("Failed to generate random string: %v", err)
	}
	for i := range b {
		b[i] = charset[b[i]%byte(len(charset))]
	}
	return string(b)
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// SOCKS5 credentials - use env vars or generate random ones
	socks5Username = os.Getenv("PROXY_USER")
	socks5Password = os.Getenv("PROXY_PASS")
	
	if socks5Username == "" {
		socks5Username = generateRandomString(8)
	}
	if socks5Password == "" {
		socks5Password = generateRandomString(16)
	}

	proxy := &ProxyServer{
		username: socks5Username,
		password: socks5Password,
	}

	// Print startup information
	log.Printf("🚀 Proxy server [Instance: %s] starting on port %s", instanceID, port)
	
	// Print SOCKS5 connection information
	printSOCKS5ConnectionInfo(port)

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
	
	// Peek at the first byte to detect protocol
	// HTTP starts with method name (GET, POST, CONNECT, etc.)
	// SOCKS5 starts with 0x05 (version byte)
	buf := make([]byte, 1)
	n, err := conn.Read(buf)
	if err != nil {
		conn.Close()
		return
	}
	
	// Check if it's SOCKS5 (version byte 0x05)
	if n > 0 && buf[0] == 0x05 {
		handleSOCKS5Connection(conn, buf[:n], proxy)
		return
	}
	
	// Check if it looks like HTTP - need more bytes
	moreBuf := make([]byte, 7)
	n2, err := conn.Read(moreBuf)
	if err != nil {
		conn.Close()
		return
	}
	
	// Combine buffers
	fullBuf := append(buf[:n], moreBuf[:n2]...)
	
	// Check if it looks like HTTP
	isHTTP := false
	httpMethods := []string{"GET ", "POST ", "PUT ", "DELETE ", "HEAD ", "OPTIONS ", "PATCH ", "CONNECT ", "TRACE "}
	bufStr := string(fullBuf)
	for _, method := range httpMethods {
		if strings.HasPrefix(bufStr, method) {
			isHTTP = true
			break
		}
	}
	
	if isHTTP {
		// Handle as HTTP - we need to wrap the connection with the already-read bytes
		handleHTTPConnection(conn, fullBuf, proxy)
	} else {
		// Unknown protocol
		conn.Close()
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

// printSOCKS5ConnectionInfo prints the SOCKS5 connection details
func printSOCKS5ConnectionInfo(port string) {
	// Get Railway public domain
	domain := os.Getenv("RAILWAY_PUBLIC_DOMAIN")
	if domain == "" {
		domain = os.Getenv("RAILWAY_STATIC_URL")
		// Strip protocol if present (RAILWAY_STATIC_URL may include https://)
		domain = strings.TrimPrefix(domain, "https://")
		domain = strings.TrimPrefix(domain, "http://")
	}
	
	log.Println("\n" + strings.Repeat("=", 80))
	log.Printf("🔐 SOCKS5 Username: %s", socks5Username)
	log.Printf("🔐 SOCKS5 Password: %s", socks5Password)
	
	if domain != "" {
		// Railway exposes services on port 443 externally via HTTPS
		telegramURL := fmt.Sprintf("https://t.me/socks?server=%s&port=443&user=%s&pass=%s", 
			domain, socks5Username, socks5Password)
		log.Printf("📱 Telegram Connection URL: %s", telegramURL)
		log.Println("\n✅ Telegram Setup Instructions:")
		log.Println("   1. Open Telegram > Settings > Data and Storage > Proxy Settings")
		log.Println("   2. Tap 'Add Proxy' or use the URL above")
		log.Println("   3. If adding manually:")
		log.Printf("      - Type: SOCKS5")
		log.Printf("      - Server: %s", domain)
		log.Printf("      - Port: 443")
		log.Printf("      - Username: %s", socks5Username)
		log.Printf("      - Password: %s", socks5Password)
	} else {
		log.Println("⚠️  Railway domain not detected. Set RAILWAY_PUBLIC_DOMAIN or RAILWAY_STATIC_URL")
		log.Printf("📱 Telegram Connection Format: https://t.me/socks?server=<your-domain>&port=443&user=%s&pass=%s", 
			socks5Username, socks5Password)
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

// SOCKS5 Protocol Implementation (RFC 1928)

// SOCKS5 constants
const (
	socks5Version byte = 0x05
	
	// Authentication methods
	socks5AuthNone     byte = 0x00
	socks5AuthPassword byte = 0x02
	socks5AuthNoAccept byte = 0xFF
	
	// Commands
	socks5CmdConnect byte = 0x01
	
	// Address types
	socks5AddrIPv4   byte = 0x01
	socks5AddrDomain byte = 0x03
	socks5AddrIPv6   byte = 0x04
	
	// Reply codes
	socks5ReplySuccess              byte = 0x00
	socks5ReplyGeneralFailure       byte = 0x01
	socks5ReplyConnectionNotAllowed byte = 0x02
	socks5ReplyNetworkUnreachable   byte = 0x03
	socks5ReplyHostUnreachable      byte = 0x04
	socks5ReplyConnectionRefused    byte = 0x05
	socks5ReplyTTLExpired           byte = 0x06
	socks5ReplyCmdNotSupported      byte = 0x07
	socks5ReplyAddrNotSupported     byte = 0x08
)

// handleSOCKS5Connection handles SOCKS5 protocol connection
func handleSOCKS5Connection(conn net.Conn, initialData []byte, proxy *ProxyServer) {
	defer conn.Close()
	
	atomic.AddInt64(&activeConnections, 1)
	defer atomic.AddInt64(&activeConnections, -1)
	
	// Step 1: Read the rest of the greeting (version byte already read)
	// Format: [version=0x05] [nmethods] [methods...]
	buf := make([]byte, 257)
	copy(buf, initialData) // Copy the version byte we already read
	
	// Read nmethods
	n, err := conn.Read(buf[1:2])
	if err != nil || n != 1 {
		log.Printf("[SOCKS5] Failed to read nmethods: %v", err)
		return
	}
	
	nmethods := int(buf[1])
	if nmethods < 1 {
		log.Printf("[SOCKS5] Invalid nmethods: %d", nmethods)
		return
	}
	
	// Read methods
	n, err = io.ReadFull(conn, buf[2:2+nmethods])
	if err != nil {
		log.Printf("[SOCKS5] Failed to read methods: %v", err)
		return
	}
	
	// Step 2: Select authentication method
	// If proxy has username/password, require auth, otherwise no auth
	selectedMethod := socks5AuthNoAccept
	if proxy.username != "" && proxy.password != "" {
		// Check if client supports password auth
		for i := 0; i < nmethods; i++ {
			if buf[2+i] == socks5AuthPassword {
				selectedMethod = socks5AuthPassword
				break
			}
		}
	} else {
		// No authentication required
		for i := 0; i < nmethods; i++ {
			if buf[2+i] == socks5AuthNone {
				selectedMethod = socks5AuthNone
				break
			}
		}
	}
	
	if selectedMethod == socks5AuthNoAccept {
		conn.Write([]byte{socks5Version, socks5AuthNoAccept})
		log.Printf("[SOCKS5] No acceptable authentication method")
		return
	}
	
	// Send method selection
	if _, err := conn.Write([]byte{socks5Version, selectedMethod}); err != nil {
		log.Printf("[SOCKS5] Failed to send method selection: %v", err)
		return
	}
	
	// Step 3: Handle authentication if required
	if selectedMethod == socks5AuthPassword {
		if !handleSOCKS5Auth(conn, proxy) {
			return
		}
	}
	
	// Step 4: Read connection request
	// Format: [version] [cmd] [rsv=0x00] [atyp] [dst.addr] [dst.port]
	n, err = io.ReadFull(conn, buf[:4])
	if err != nil {
		log.Printf("[SOCKS5] Failed to read request header: %v", err)
		return
	}
	
	if buf[0] != socks5Version {
		log.Printf("[SOCKS5] Invalid version in request: %d", buf[0])
		return
	}
	
	cmd := buf[1]
	// buf[2] is reserved
	atyp := buf[3]
	
	// Only support CONNECT command
	if cmd != socks5CmdConnect {
		sendSOCKS5Reply(conn, socks5ReplyCmdNotSupported, net.IPv4zero, 0)
		log.Printf("[SOCKS5] Unsupported command: %d", cmd)
		return
	}
	
	// Step 5: Read destination address
	var host string
	switch atyp {
	case socks5AddrIPv4:
		n, err = io.ReadFull(conn, buf[:4])
		if err != nil {
			sendSOCKS5Reply(conn, socks5ReplyGeneralFailure, net.IPv4zero, 0)
			return
		}
		host = net.IP(buf[:4]).String()
		
	case socks5AddrDomain:
		n, err = io.ReadFull(conn, buf[:1])
		if err != nil {
			sendSOCKS5Reply(conn, socks5ReplyGeneralFailure, net.IPv4zero, 0)
			return
		}
		domainLen := int(buf[0])
		n, err = io.ReadFull(conn, buf[:domainLen])
		if err != nil {
			sendSOCKS5Reply(conn, socks5ReplyGeneralFailure, net.IPv4zero, 0)
			return
		}
		host = string(buf[:domainLen])
		
	case socks5AddrIPv6:
		n, err = io.ReadFull(conn, buf[:16])
		if err != nil {
			sendSOCKS5Reply(conn, socks5ReplyGeneralFailure, net.IPv4zero, 0)
			return
		}
		host = net.IP(buf[:16]).String()
		
	default:
		sendSOCKS5Reply(conn, socks5ReplyAddrNotSupported, net.IPv4zero, 0)
		log.Printf("[SOCKS5] Unsupported address type: %d", atyp)
		return
	}
	
	// Read port
	n, err = io.ReadFull(conn, buf[:2])
	if err != nil {
		sendSOCKS5Reply(conn, socks5ReplyGeneralFailure, net.IPv4zero, 0)
		return
	}
	port := int(buf[0])<<8 | int(buf[1])
	
	// Step 6: Connect to target
	target := fmt.Sprintf("%s:%d", host, port)
	log.Printf("[SOCKS5] [%s] Connecting to %s", instanceID, target)
	
	targetConn, err := net.DialTimeout("tcp", target, 30*time.Second)
	if err != nil {
		log.Printf("[SOCKS5] [%s] Failed to connect to %s: %v", instanceID, target, err)
		replyCode := socks5ReplyHostUnreachable
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			replyCode = socks5ReplyTTLExpired
		}
		sendSOCKS5Reply(conn, replyCode, net.IPv4zero, 0)
		return
	}
	defer targetConn.Close()
	
	// Send success reply
	localAddr := targetConn.LocalAddr().(*net.TCPAddr)
	sendSOCKS5Reply(conn, socks5ReplySuccess, localAddr.IP, localAddr.Port)
	
	log.Printf("[SOCKS5] [%s] Connection established to %s", instanceID, target)
	
	// Step 7: Relay traffic bidirectionally
	done := make(chan bool, 2)
	
	go func() {
		io.Copy(targetConn, conn)
		done <- true
	}()
	
	go func() {
		io.Copy(conn, targetConn)
		done <- true
	}()
	
	<-done
	log.Printf("[SOCKS5] [%s] Connection closed to %s", instanceID, target)
}

// handleSOCKS5Auth handles SOCKS5 username/password authentication
func handleSOCKS5Auth(conn net.Conn, proxy *ProxyServer) bool {
	// Format: [version=0x01] [ulen] [username] [plen] [password]
	buf := make([]byte, 513)
	
	// Read version and username length
	n, err := io.ReadFull(conn, buf[:2])
	if err != nil || n != 2 {
		log.Printf("[SOCKS5] Failed to read auth version: %v", err)
		return false
	}
	
	if buf[0] != 0x01 {
		log.Printf("[SOCKS5] Invalid auth version: %d", buf[0])
		return false
	}
	
	ulen := int(buf[1])
	if ulen < 1 {
		conn.Write([]byte{0x01, 0x01}) // Auth failed
		return false
	}
	
	// Read username
	n, err = io.ReadFull(conn, buf[:ulen])
	if err != nil {
		log.Printf("[SOCKS5] Failed to read username: %v", err)
		return false
	}
	username := string(buf[:ulen])
	
	// Read password length
	n, err = io.ReadFull(conn, buf[:1])
	if err != nil {
		log.Printf("[SOCKS5] Failed to read password length: %v", err)
		return false
	}
	plen := int(buf[0])
	
	// Read password
	n, err = io.ReadFull(conn, buf[:plen])
	if err != nil {
		log.Printf("[SOCKS5] Failed to read password: %v", err)
		return false
	}
	password := string(buf[:plen])
	
	// Verify credentials
	if username == proxy.username && password == proxy.password {
		conn.Write([]byte{0x01, 0x00}) // Auth success
		return true
	}
	
	log.Printf("[SOCKS5] Authentication failed for user: %s", username)
	conn.Write([]byte{0x01, 0x01}) // Auth failed
	return false
}

// sendSOCKS5Reply sends a SOCKS5 reply message
func sendSOCKS5Reply(conn net.Conn, reply byte, ip net.IP, port int) {
	// Format: [version] [reply] [rsv=0x00] [atyp] [bind.addr] [bind.port]
	resp := make([]byte, 4)
	resp[0] = socks5Version
	resp[1] = reply
	resp[2] = 0x00 // Reserved
	
	// Determine address type and format
	if ip4 := ip.To4(); ip4 != nil {
		resp[3] = socks5AddrIPv4
		resp = append(resp, ip4...)
	} else if ip6 := ip.To16(); ip6 != nil {
		resp[3] = socks5AddrIPv6
		resp = append(resp, ip6...)
	} else {
		resp[3] = socks5AddrIPv4
		resp = append(resp, net.IPv4zero...)
	}
	
	// Add port
	resp = append(resp, byte(port>>8), byte(port&0xFF))
	
	conn.Write(resp)
}
