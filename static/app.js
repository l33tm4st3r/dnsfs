// DNSFS Client App

document.addEventListener('DOMContentLoaded', () => {
    // DOM Elements
    const hostAddrBadge = document.getElementById('host-addr-badge');
    const dnsZoneBadge = document.getElementById('dns-zone-badge');
    
    const statFilesCount = document.getElementById('stat-files-count');
    const statChunksCount = document.getElementById('stat-chunks-count');
    const statResolversCount = document.getElementById('stat-resolvers-count');
    const statLatencyValue = document.getElementById('stat-latency-value');
    
    const dropZone = document.getElementById('drop-zone');
    const fileInput = document.getElementById('file-input');
    const progressContainer = document.getElementById('progress-container');
    const progressFileName = document.getElementById('progress-file-name');
    const progressFileSize = document.getElementById('progress-file-size');
    const progressBar = document.getElementById('progress-bar');
    const progressChunksSent = document.getElementById('progress-chunks-sent');
    const progressPercent = document.getElementById('progress-percent');
    
    const fetchFilenameInput = document.getElementById('fetch-filename');
    const btnFetchManual = document.getElementById('btn-fetch-manual');
    const fileListTbody = document.getElementById('file-list-tbody');
    
    const resolverGrid = document.getElementById('resolver-grid');
    const btnTestResolvers = document.getElementById('btn-test-resolvers');
    
    const logConsole = document.getElementById('log-console');
    const autoscrollCheck = document.getElementById('autoscroll-check');
    
    let isUploading = false;
    let knownLogTimestamps = new Set();

    // 1. Polling stats, resolvers, and logs
    async function updateStats() {
        try {
            const res = await fetch('/api/stats');
            if (!res.ok) return;
            const data = await res.json();
            
            // Badges
            hostAddrBadge.textContent = data.addr || '127.0.0.1';
            dnsZoneBadge.textContent = data.dbase || 's.flm.me.uk';
            
            // Stats counts
            statFilesCount.textContent = data.files ? data.files.length : 0;
            
            let totalChunks = 0;
            if (data.files) {
                data.files.forEach(f => totalChunks += f.chunks);
            }
            statChunksCount.textContent = totalChunks;
            statResolversCount.textContent = data.resolverCount || 0;
            
            // Populate file table
            populateFileTable(data.files || []);
        } catch (err) {
            console.error('Failed to fetch stats:', err);
        }
    }

    async function updateResolvers() {
        try {
            const res = await fetch('/api/resolvers');
            if (!res.ok) return;
            const list = await res.json();
            
            // Sort resolvers by IP
            list.sort((a, b) => a.ip.localeCompare(b.ip, undefined, { numeric: true, sensitivity: 'base' }));
            
            // Calculate average latency of active resolvers
            let latSum = 0;
            let latCount = 0;
            
            resolverGrid.innerHTML = '';
            list.forEach(r => {
                const node = document.createElement('div');
                node.className = `resolver-node status-${(r.status || 'unknown').toLowerCase()}`;
                
                const latText = r.latencyMs > 0 ? `${r.latencyMs.toFixed(1)} ms` : (r.status === 'Inactive' ? 'Offline' : 'Unknown');
                if (r.latencyMs > 0 && r.status === 'Active') {
                    latSum += r.latencyMs;
                    latCount++;
                }

                node.innerHTML = `
                    <span class="resolver-ip" title="${r.ip}">${r.ip}</span>
                    <span class="resolver-latency">${latText}</span>
                    <span class="resolver-requests">Reqs: ${r.requests || 0}</span>
                `;
                resolverGrid.appendChild(node);
            });

            if (latCount > 0) {
                statLatencyValue.textContent = `${(latSum / latCount).toFixed(1)} ms`;
            } else {
                statLatencyValue.textContent = '-- ms';
            }
        } catch (err) {
            console.error('Failed to fetch resolvers:', err);
        }
    }

    async function updateLogs() {
        try {
            const res = await fetch('/api/logs');
            if (!res.ok) return;
            const logs = await res.json();
            
            let newLogsAdded = false;
            logs.forEach(log => {
                // To avoid duplicate lines in console
                const key = `${log.timestamp}-${log.message}`;
                if (!knownLogTimestamps.has(key)) {
                    knownLogTimestamps.add(key);
                    
                    const logLine = document.createElement('div');
                    logLine.className = 'log-line';
                    
                    // Style lines based on content keywords
                    const lowerMsg = log.message.toLowerCase();
                    if (lowerMsg.includes('warning') || lowerMsg.includes('oops') || lowerMsg.includes('timed out')) {
                        logLine.classList.add('warning');
                    } else if (lowerMsg.includes('error') || lowerMsg.includes('failed')) {
                        logLine.classList.add('error');
                    } else if (lowerMsg.includes('success') || lowerMsg.includes('cached') || lowerMsg.includes('retrieved')) {
                        logLine.classList.add('success');
                    } else if (lowerMsg.includes('started') || lowerMsg.includes('initializing') || lowerMsg.includes('triggered')) {
                        logLine.classList.add('system');
                    }
                    
                    logLine.innerHTML = `<span style="color: #4a5568">[${log.timestamp}]</span> ${escapeHtml(log.message)}`;
                    logConsole.appendChild(logLine);
                    newLogsAdded = true;
                }
            });

            if (newLogsAdded && autoscrollCheck.checked) {
                logConsole.scrollTop = logConsole.scrollHeight;
            }
        } catch (err) {
            console.error('Failed to fetch logs:', err);
        }
    }

    function populateFileTable(files) {
        if (files.length === 0) {
            fileListTbody.innerHTML = `
                <tr class="empty-row">
                    <td colspan="4">No files cached in this session yet.</td>
                </tr>
            `;
            return;
        }

        fileListTbody.innerHTML = '';
        files.forEach(f => {
            const tr = document.createElement('tr');
            
            const sizeStr = formatBytes(f.size);
            const dateStr = new Date(f.createdAt).toLocaleTimeString();
            
            tr.innerHTML = `
                <td class="file-name-cell">${escapeHtml(f.name)}</td>
                <td>${sizeStr}</td>
                <td>${f.chunks}</td>
                <td>
                    <button class="cyber-button-sm btn-download" data-filename="${escapeHtml(f.name)}">Fetch</button>
                </td>
            `;
            fileListTbody.appendChild(tr);
        });

        // Add event listeners to download buttons
        document.querySelectorAll('.btn-download').forEach(btn => {
            btn.addEventListener('click', (e) => {
                const filename = e.target.getAttribute('data-filename');
                downloadFile(filename);
            });
        });
    }

    // 2. Upload handler
    async function uploadFile(file) {
        if (isUploading) return;
        isUploading = true;
        
        // Show progress bar
        progressContainer.classList.remove('hidden');
        progressFileName.textContent = file.name;
        progressFileSize.textContent = formatBytes(file.size);
        progressBar.style.width = '0%';
        progressPercent.textContent = '0%';
        
        const chunkCount = Math.ceil(file.size / 180);
        progressChunksSent.textContent = `0 / ${chunkCount} Chunks`;
        
        // We will fake a smooth progress animation because the backend handles the uploading loop internally
        let percent = 0;
        const interval = setInterval(() => {
            if (percent < 90) {
                percent += Math.ceil((100 - percent) * 0.1);
                progressBar.style.width = `${percent}%`;
                progressPercent.textContent = `${percent}%`;
                progressChunksSent.textContent = `${Math.ceil(chunkCount * (percent / 100))} / ${chunkCount} Chunks`;
            }
        }, 150);

        try {
            const response = await fetch(`/upload?name=${encodeURIComponent(file.name)}`, {
                method: 'POST',
                body: file
            });

            clearInterval(interval);

            if (response.ok) {
                progressBar.style.width = '100%';
                progressPercent.textContent = '100%';
                progressChunksSent.textContent = `${chunkCount} / ${chunkCount} Chunks`;
                progressBar.style.background = 'var(--primary)';
                
                setTimeout(() => {
                    progressContainer.classList.add('hidden');
                    isUploading = false;
                    updateStats();
                }, 1500);
            } else {
                throw new Error('Upload failed on server.');
            }
        } catch (err) {
            clearInterval(interval);
            progressBar.style.width = '100%';
            progressBar.style.background = 'var(--red)';
            progressPercent.textContent = 'ERROR';
            progressChunksSent.textContent = 'Upload failed';
            console.error('Upload error:', err);
            
            setTimeout(() => {
                progressContainer.classList.add('hidden');
                isUploading = false;
            }, 3000);
        }
    }

    // 3. Download handler
    async function downloadFile(filename) {
        addManualLog(`Triggered client fetch and reassembly for file: '${filename}'`);
        try {
            const res = await fetch(`/fetch?name=${encodeURIComponent(filename)}`);
            if (res.status === 404) {
                alert(`File "${filename}" was not found in resolver caches or cached chunks have expired.`);
                return;
            }
            if (!res.ok) throw new Error('Failed to download file');
            
            const blob = await res.blob();
            const url = window.URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = filename;
            document.body.appendChild(a);
            a.click();
            document.body.removeChild(a);
            window.URL.revokeObjectURL(url);
        } catch (err) {
            console.error('Download error:', err);
            alert(`Error fetching file: ${err.message}`);
        }
    }

    // 4. Test resolvers handler
    btnTestResolvers.addEventListener('click', async () => {
        btnTestResolvers.disabled = true;
        btnTestResolvers.textContent = 'Validating Nodes...';
        
        try {
            const res = await fetch('/api/resolvers/test', { method: 'POST' });
            if (res.ok) {
                // Immediately refresh resolvers grid
                await updateResolvers();
            }
        } catch (err) {
            console.error('Latency test error:', err);
        } finally {
            btnTestResolvers.disabled = false;
            btnTestResolvers.textContent = 'Ping & Validate Cache Nodes';
        }
    });

    // 5. Drag and Drop events
    dropZone.addEventListener('click', () => fileInput.click());
    
    fileInput.addEventListener('change', (e) => {
        if (e.target.files.length > 0) {
            uploadFile(e.target.files[0]);
        }
    });

    ['dragenter', 'dragover'].forEach(eventName => {
        dropZone.addEventListener(eventName, (e) => {
            e.preventDefault();
            dropZone.classList.add('dragover');
        }, false);
    });

    ['dragleave', 'drop'].forEach(eventName => {
        dropZone.addEventListener(eventName, (e) => {
            e.preventDefault();
            dropZone.classList.remove('dragover');
        }, false);
    });

    dropZone.addEventListener('drop', (e) => {
        const dt = e.dataTransfer;
        const files = dt.files;
        if (files.length > 0) {
            uploadFile(files[0]);
        }
    });

    // Manual Fetch Form
    btnFetchManual.addEventListener('click', () => {
        const filename = fetchFilenameInput.value.trim();
        if (filename) {
            downloadFile(filename);
            fetchFilenameInput.value = '';
        }
    });

    fetchFilenameInput.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') {
            btnFetchManual.click();
        }
    });

    // Helper functions
    function formatBytes(bytes, decimals = 2) {
        if (bytes === 0) return '0 Bytes';
        const k = 1024;
        const dm = decimals < 0 ? 0 : decimals;
        const sizes = ['Bytes', 'KB', 'MB', 'GB'];
        const i = Math.floor(Math.log(bytes) / Math.log(k));
        return parseFloat((bytes / Math.pow(k, i)).toFixed(dm)) + ' ' + sizes[i];
    }

    function escapeHtml(text) {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }

    function addManualLog(message) {
        const time = new Date().toLocaleTimeString();
        const logLine = document.createElement('div');
        logLine.className = 'log-line system';
        logLine.innerHTML = `<span style="color: #4a5568">[${time}]</span> CLIENT: ${escapeHtml(message)}`;
        logConsole.appendChild(logLine);
        if (autoscrollCheck.checked) {
            logConsole.scrollTop = logConsole.scrollHeight;
        }
    }

    // Initialization and loop
    updateStats();
    updateResolvers();
    updateLogs();

    setInterval(() => {
        updateStats();
        updateLogs();
    }, 1500);

    // Resolvers updated less frequently to avoid flooding
    setInterval(() => {
        updateResolvers();
    }, 4500);
});
