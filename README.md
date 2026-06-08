# dnsfs

Store your data in others' DNS resolvers cache, True cloud storage!

See the original blog post: https://blog.benjojo.co.uk/post/dns-filesystem-true-cloud-storage-dnsfs

---

## Features

- **Chunked UDP DNS Storage**: Files are split into 180-byte chunks, Base64-encoded, and cached in public DNS resolvers as `TXT` records.
- **Redundancy & Replication**: Shards are replicated across multiple DNS resolvers to prevent data loss.
- **Premium Web Dashboard**: A built-in user interface to drag-and-drop uploads, fetch and download files, manage DNS resolvers, and view real-time query logs.
- **Mock Testing Mode**: Offline-friendly loopback mode to test the system end-to-end without needing root privileges, a registered domain name, or internet access.

---

## Compilation

To compile all binaries, initialize dependencies and build the main executable:

```bash
# Get dependencies and compile
go mod tidy
go build -o bin/dnsfs ./dnsfs
```

To build other utility tools (like the masscan parser or retention check):
```bash
make all
```

---

## Local Mock & Development Mode

You can run DNSFS on a local port without root privileges. In this mode, DNSFS routes UDP packets to its own DNS listen port, and chunks are cached in-memory without deletion:

1. **Start the server**:
   ```bash
   ./bin/dnsfs -addr 127.0.0.1 -dnsport 5300 -file iplist.txt -dbase s.flm.me.uk -mock
   ```

2. **Access the Web Dashboard**:
   Open your browser and navigate to: **[http://127.0.0.1:5050](http://127.0.0.1:5050)**

---

## Production Deployment

To deploy DNSFS in production using public DNS resolver caches:

### Prerequisites
1. **Registered Domain**: A domain name (e.g., `yourdomain.com`).
2. **NS Record**: Delegate a subdomain (e.g., `s.yourdomain.com`) with an NS record pointing to the public IP of the machine running DNSFS.
3. **Open Port 53**: Ensure port 53 (UDP) is open and accessible from the public internet.

### Running the Server
Since binding to port 53 is a privileged operation, run the binary as root or with `CAP_NET_BIND_SERVICE`:

```bash
sudo ./bin/dnsfs -addr <YOUR_PUBLIC_IP> -dnsport 53 -file iplist.txt -dbase s.yourdomain.com
```

Where `iplist.txt` is a text file containing the IP addresses of open/recursive DNS resolvers (one per line).

---

## Usage & Examples

You can interact with DNSFS either through the **Web Dashboard** or using command-line utilities.

### 1. Uploading Files

**Using the Web Dashboard**:
Simply drag and drop any file into the upload zone on the web interface, or click to select a file.

**Using curl**:
You can upload files directly to the HTTP API endpoint:
```bash
curl -X POST --data-binary @my_document.pdf "http://127.0.0.1:5050/upload?name=my_document.pdf"
```

### 2. Fetching & Reassembling Files

**Using the Web Dashboard**:
Click the **Fetch** button next to any file in the inventory, or enter the filename manually in the input box.

**Using curl**:
Retrieve and reassemble the file:
```bash
curl -o downloaded_document.pdf "http://127.0.0.1:5050/fetch?name=my_document.pdf"
```

### 3. Monitoring APIs

- **Stats & Inventory**: `GET /api/stats`
- **Resolver Nodes**: `GET /api/resolvers`
- **Logger Console**: `GET /api/logs`

---

## How It Works Under the Hood

1. **Upload**: When a file is uploaded, the server slices it into 180-byte chunks.
2. **Sharding**: The chunk data is base64-encoded, and the server calculates the target resolver IPs by hashing the filename and chunk index.
3. **Caching**: The server sends a recursive query for `dfs-<hash>.s.flm.me.uk.` to the target DNS resolvers.
4. **Resolution**: The resolvers, not having the record cached, query our authoritative DNSFS server. DNSFS returns the base64-encoded chunk content as a `TXT` record with a maximum TTL (e.g., 2,147,483,646 seconds).
5. **Retrieval**: When fetching the file, the server queries the same resolvers. The resolvers respond instantly with the cached `TXT` record without asking DNSFS again.
