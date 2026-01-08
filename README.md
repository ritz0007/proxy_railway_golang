# Go Proxy Server for Railway.com

A high-performance HTTP/HTTPS and SOCKS5 proxy server written in Go, designed for deployment on Railway.com with multiple instance support. Now includes support for Telegram clients via SOCKS5 protocol!

## Features

- ✅ HTTP and HTTPS (CONNECT) proxy support
- ✅ **SOCKS5 proxy for Telegram** (NEW!)
- ✅ Multiple instance support with unique instance IDs
- ✅ Optional username/password authentication
- ✅ Auto-generated credentials if not configured
- ✅ Health check endpoint for Railway
- ✅ Connection statistics tracking
- ✅ Request forwarding headers (X-Forwarded-For)
- ✅ Automatic scaling with Railway replicas

## Why SOCKS5 Instead of MTProto?

This proxy uses **SOCKS5** rather than MTProto for Telegram connections because:

- **Railway HTTPS Termination**: Railway terminates TLS/HTTPS at the edge, which conflicts with MTProto's raw TCP requirements
- **Better Compatibility**: SOCKS5 works seamlessly with Railway's infrastructure
- **Universal Support**: Works with Telegram and other applications
- **Simpler Implementation**: RFC 1928 standard protocol with proven reliability
- **Secure**: Supports username/password authentication, plus Railway's TLS encryption

## Deployment on Railway

### Method 1: Deploy via GitHub

1. Push this code to a GitHub repository
2. Go to [Railway.com](https://railway.app)
3. Click "New Project" → "Deploy from GitHub repo"
4. Select your repository
5. Railway will automatically detect the Dockerfile and deploy
6. **Check the deployment logs for your Telegram connection URL and credentials!**

### Method 2: Deploy via Railway CLI

```bash
# Install Railway CLI
npm install -g @railway/cli

# Login to Railway
railway login

# Initialize and deploy
railway init
railway up
```

### Finding Your SOCKS5 Credentials

After deployment, check the Railway logs to find your connection information:

1. Go to your Railway project dashboard
2. Click on your service
3. Navigate to the "Deployments" tab
4. Click on the latest deployment
5. Look for the connection information in the logs:
   ```
   🔐 SOCKS5 Username: <username>
   🔐 SOCKS5 Password: <password>
   📱 Telegram Connection URL: https://t.me/socks?server=...
   ```

## Environment Variables

Set these in Railway dashboard under "Variables":

| Variable | Description | Required |
|----------|-------------|----------|
| `PORT` | Server port (auto-set by Railway) | No |
| `PROXY_USER` | Username for proxy authentication (auto-generated if not set) | No |
| `PROXY_PASS` | Password for proxy authentication (auto-generated if not set) | No |

**Note**: If you don't set `PROXY_USER` and `PROXY_PASS`, the server will automatically generate random credentials on startup. Check the deployment logs to see the generated credentials.

## Multiple Instances

To run multiple instances on Railway:

1. Go to your Railway project settings
2. Navigate to the "Settings" tab
3. Under "Scaling", set the number of replicas (e.g., 3)
4. Or modify `railway.toml` and set `numReplicas = 3`

Railway will automatically load balance between instances.

## Endpoints

| Endpoint | Description |
|----------|-------------|
| `/health` | Health check (returns JSON status) |
| `/stats` | Server statistics |
| All other traffic | Proxied based on protocol detection (HTTP/HTTPS/SOCKS5) |

## SOCKS5 Proxy for Telegram

### How It Works

The server automatically generates or uses configured credentials for SOCKS5 authentication:
- **Auto-generated**: Random 8-character username and 16-character password if not configured
- **Configured**: Uses `PROXY_USER` and `PROXY_PASS` environment variables if set
- Each Railway replica instance can have its own credentials
- Railway's HTTPS termination provides encryption for the proxy connection

### Connection Format

```
https://t.me/socks?server=<your-domain>&port=443&user=<username>&pass=<password>
```

Example:
```
https://t.me/socks?server=my-proxy.up.railway.app&port=443&user=admin&pass=secret123
```

### Connecting Telegram to Your SOCKS5 Proxy

**Method 1: One-Click Setup (Easiest)**

1. Deploy the server on Railway
2. Check the deployment logs for your Telegram connection URL
3. Open the URL on your mobile device (it will look like):
   ```
   https://t.me/socks?server=your-app.up.railway.app&port=443&user=xxx&pass=yyy
   ```
4. Telegram will open and ask you to confirm adding the proxy
5. Tap "Connect" and you're done!

**Method 2: Manual Setup**

1. Open Telegram
2. Go to **Settings** → **Data and Storage** → **Proxy Settings**
3. Tap **Add Proxy**
4. Select **SOCKS5**
5. Enter the connection details from your Railway deployment logs:
   - **Server**: your-app.up.railway.app
   - **Port**: 443
   - **Username**: (from logs)
   - **Password**: (from logs)
6. Tap **Save** and enable the proxy

### Multiple Instances

When running multiple Railway replicas:
- Each instance generates its own unique credentials (if not configured via env vars)
- Check logs for each instance's connection details
- Railway load balancer distributes connections
- Use configured `PROXY_USER` and `PROXY_PASS` for consistent credentials across all instances

## Usage Examples

### Using as HTTP/HTTPS Proxy

#### Without Authentication (if credentials not set)

```bash
# HTTP Proxy
curl -x https://your-app.railway.app:443 http://example.com

# HTTPS Proxy
curl -x https://your-app.railway.app:443 https://example.com
```

#### With Authentication

Set `PROXY_USER` and `PROXY_PASS` environment variables in Railway, then:

```bash
# Using curl
curl -x https://user:pass@your-app.railway.app:443 http://example.com

# Using environment variable
export http_proxy=https://user:pass@your-app.railway.app:443
export https_proxy=https://user:pass@your-app.railway.app:443
curl http://example.com
```

**Note**: 
- HTTP/HTTPS proxy uses the same credentials as SOCKS5
- If credentials are auto-generated, check deployment logs for the username/password

### Testing SOCKS5 Proxy

```bash
# Test SOCKS5 connection with curl
curl -x socks5://username:password@your-app.railway.app:443 https://api.telegram.org

# Test with any SOCKS5-compatible application
# Example: SSH over SOCKS5
ssh -o ProxyCommand="nc -X 5 -x your-app.railway.app:443 %h %p" user@remote-host
```

### Python Example

```python
import requests

proxies = {
    'http': 'https://user:pass@your-app.railway.app:443',
    'https': 'https://user:pass@your-app.railway.app:443',
}

response = requests.get('https://api.ipify.org', proxies=proxies)
print(response.text)
```

### Node.js Example

```javascript
const HttpsProxyAgent = require('https-proxy-agent');
const fetch = require('node-fetch');

const agent = new HttpsProxyAgent('https://user:pass@your-app.railway.app:443');

fetch('https://api.ipify.org', { agent })
  .then(res => res.text())
  .then(console.log);
```

## Local Development

```bash
# Run locally
go run main.go

# With custom credentials
PROXY_USER=admin PROXY_PASS=secret go run main.go

# Build binary
go build -o proxy-server .
./proxy-server
```

## Protocol Support

This proxy server intelligently detects and handles multiple protocols:

1. **SOCKS5**: Detected by initial byte `0x05`
   - Full RFC 1928 implementation
   - Username/password authentication (RFC 1929)
   - Supports CONNECT command for TCP relay
   - Works with Telegram and other SOCKS5 clients

2. **HTTP/HTTPS**: Detected by HTTP method names (GET, POST, CONNECT, etc.)
   - Standard HTTP proxy for web traffic
   - HTTPS tunneling via CONNECT method
   - Basic authentication support

3. **Health/Stats Endpoints**: HTTP-based monitoring
   - `/health` - Health check for Railway
   - `/stats` - Connection and request statistics

## Architecture

```
                    ┌─────────────────────┐
                    │   Railway Load      │
                    │   Balancer (TLS)    │
                    └─────────┬───────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        │                     │                     │
        ▼                     ▼                     ▼
┌───────────────┐     ┌───────────────┐     ┌───────────────┐
│   Instance 1  │     │   Instance 2  │     │   Instance 3  │
│  SOCKS5/HTTP  │     │  SOCKS5/HTTP  │     │  SOCKS5/HTTP  │
└───────────────┘     └───────────────┘     └───────────────┘
```

**Key Points:**
- Railway provides TLS termination at the load balancer
- SOCKS5 works over Railway's HTTPS infrastructure
- Each instance can handle both SOCKS5 and HTTP/HTTPS protocols
- Protocol detection happens at connection time based on first byte(s)

## Security Considerations

- **TLS Encryption**: Railway terminates TLS at the edge, providing encryption for all connections
- **Authentication**: Username/password authentication prevents unauthorized access
- **Credential Generation**: Auto-generated strong passwords if not configured
- **No Secret Storage**: Credentials are only displayed in deployment logs, not stored in files
- **Instance Isolation**: Each replica runs independently with its own credentials (if not configured globally)

## Troubleshooting

### Telegram Won't Connect

1. **Check credentials**: Make sure you're using the correct username/password from the deployment logs
2. **Verify domain**: Ensure you're using the correct Railway domain (check `RAILWAY_PUBLIC_DOMAIN`)
3. **Port number**: Should always be 443 (Railway's HTTPS port)
4. **Proxy type**: Make sure you selected "SOCKS5" not "SOCKS4" or "HTTP"

### HTTP/HTTPS Proxy Issues

1. **Authentication**: Check if `PROXY_USER` and `PROXY_PASS` are required
2. **Protocol**: Make sure you're using the correct protocol (http:// vs https://)
3. **Check logs**: Railway deployment logs show connection attempts and errors

### Finding Credentials

1. Go to Railway dashboard → Your service → Deployments
2. Click on the latest deployment
3. Look for lines starting with `🔐 SOCKS5 Username:` and `🔐 SOCKS5 Password:`

## License

MIT License
