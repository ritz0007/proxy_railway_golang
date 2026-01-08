# Go Proxy Server for Railway.com

A high-performance HTTP/HTTPS and MTProto proxy server written in Go, designed for deployment on Railway.com with multiple instance support. Now includes support for Telegram clients via MTProto protocol!

## Features

- ✅ HTTP and HTTPS (CONNECT) proxy support
- ✅ **MTProto proxy for Telegram** (NEW!)
- ✅ Multiple instance support with unique instance IDs
- ✅ Optional Basic authentication
- ✅ Health check endpoint for Railway
- ✅ Connection statistics tracking
- ✅ Request forwarding headers (X-Forwarded-For)
- ✅ Automatic scaling with Railway replicas
- ✅ **Instance-specific MTProto secrets**

## Deployment on Railway

### Method 1: Deploy via GitHub

1. Push this code to a GitHub repository
2. Go to [Railway.com](https://railway.app)
3. Click "New Project" → "Deploy from GitHub repo"
4. Select your repository
5. Railway will automatically detect the Dockerfile and deploy
6. **Check the deployment logs for your Telegram connection URL!**

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

### Finding Your Telegram Connection URL

After deployment, check the Railway logs to find your connection information:

1. Go to your Railway project dashboard
2. Click on your service
3. Navigate to the "Deployments" tab
4. Click on the latest deployment
5. Look for the connection URL in the logs, formatted as:
   ```
   📱 Telegram Connection URL: https://t.me/proxy?server=your-app.up.railway.app&port=443&secret=...
   ```

## Environment Variables

Set these in Railway dashboard under "Variables":

| Variable | Description | Required |
|----------|-------------|----------|
| `PORT` | Server port (auto-set by Railway) | No |
| `PROXY_USER` | Username for proxy authentication | No |
| `PROXY_PASS` | Password for proxy authentication | No |

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

## MTProto Proxy Details

### How It Works

The server generates a unique 32-byte (64 hex character) secret on startup. This secret is used to:
- Authenticate Telegram clients
- Obfuscate traffic between client and proxy
- Each Railway replica instance gets its own unique secret

### Connection Format

```
https://t.me/proxy?server=<your-domain>&port=443&secret=<64-hex-chars>
```

Example:
```
https://t.me/proxy?server=my-proxy.up.railway.app&port=443&secret=1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef
```

### Telegram Datacenters

The proxy automatically connects to Telegram's datacenters:
- 149.154.175.50:443
- 149.154.167.51:443
- 149.154.175.100:443
- 149.154.167.91:443
- 149.154.171.5:443

### Multiple Instances

When running multiple Railway replicas:
- Each instance generates its own unique secret
- Check logs for each instance's connection URL
- Railway load balancer distributes connections
- Each instance ID is logged with connection details

## Usage Examples

### Connecting Telegram

**This is the easiest way to use the proxy!**

1. Deploy the server on Railway
2. Check the deployment logs for your Telegram connection URL
3. Open the URL on your mobile device (it will look like):
   ```
   https://t.me/proxy?server=your-app.up.railway.app&port=443&secret=abc123...
   ```
4. Telegram will open and ask you to confirm adding the proxy
5. Tap "Connect" and you're done!

**What is MTProto vs HTTP Proxy?**

- **MTProto Proxy**: Native Telegram protocol, optimized for Telegram clients. Works seamlessly with one-click setup via t.me link. This is what you should use for Telegram!
- **HTTP/HTTPS Proxy**: General-purpose proxy for web browsers and other applications. Can proxy any HTTP/HTTPS traffic.

This server supports both, so you can use it for Telegram AND as a regular web proxy!

### Using as HTTP/HTTPS Proxy

#### Without Authentication

```bash
# HTTP Proxy
curl -x https://your-app.railway.app:443 http://example.com

# HTTPS Proxy
curl -x https://your-app.railway.app:443 https://example.com
```

##### With Authentication

Set `PROXY_USER` and `PROXY_PASS` environment variables, then:

```bash
# Using curl
curl -x https://user:pass@your-app.railway.app:443 http://example.com

# Using environment variable
export http_proxy=https://user:pass@your-app.railway.app:443
export https_proxy=https://user:pass@your-app.railway.app:443
curl http://example.com
```

**Note**: Authentication only applies to HTTP/HTTPS proxy. MTProto connections use the secret key for authentication.

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

# With authentication
PROXY_USER=admin PROXY_PASS=secret go run main.go

# Build binary
go build -o proxy-server .
./proxy-server
```

## Architecture

```
                    ┌─────────────────────┐
                    │   Railway Load      │
                    │     Balancer        │
                    └─────────┬───────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        │                     │                     │
        ▼                     ▼                     ▼
┌───────────────┐     ┌───────────────┐     ┌───────────────┐
│   Instance 1  │     │   Instance 2  │     │   Instance 3  │
│   (Replica)   │     │   (Replica)   │     │   (Replica)   │
└───────────────┘     └───────────────┘     └───────────────┘
```

## License

MIT License
