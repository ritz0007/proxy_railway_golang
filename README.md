# Go Proxy Server for Railway.com

A high-performance HTTP/HTTPS proxy server written in Go, designed for deployment on Railway.com with multiple instance support.

## Features

- ✅ HTTP and HTTPS (CONNECT) proxy support
- ✅ Multiple instance support with unique instance IDs
- ✅ Optional Basic authentication
- ✅ Health check endpoint for Railway
- ✅ Connection statistics tracking
- ✅ Request forwarding headers (X-Forwarded-For)
- ✅ Automatic scaling with Railway replicas

## Deployment on Railway

### Method 1: Deploy via GitHub

1. Push this code to a GitHub repository
2. Go to [Railway.com](https://railway.app)
3. Click "New Project" → "Deploy from GitHub repo"
4. Select your repository
5. Railway will automatically detect the Dockerfile and deploy

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

## Usage Examples

### Without Authentication

```bash
# HTTP Proxy
curl -x https://your-app.railway.app:443 http://example.com

# HTTPS Proxy
curl -x https://your-app.railway.app:443 https://example.com
```

### With Authentication

Set `PROXY_USER` and `PROXY_PASS` environment variables, then:

```bash
# Using curl
curl -x https://user:pass@your-app.railway.app:443 http://example.com

# Using environment variable
export http_proxy=https://user:pass@your-app.railway.app:443
export https_proxy=https://user:pass@your-app.railway.app:443
curl http://example.com
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
