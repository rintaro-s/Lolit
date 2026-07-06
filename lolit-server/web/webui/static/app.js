function lolit() {
    return {
        page: 'dashboard',
        repo: localStorage.getItem('lolit:repo') || 'team/robot2026',
        user: localStorage.getItem('lolit:user') || 'me',
        query: '',
        view: 'list',
        currentPath: '',
        files: [],
        locks: [],
        commits: [],
        releases: [],
        dashboard: { lock_count: 0, commits: [] },
        searchResults: [],
        selectedFile: null,
        fileDetail: {},
        showUpload: false,
        showSettings: false,
        showRelease: false,
        newRelease: { tag: '', commit: '', note: '' },
        usagePercent: 35,
        storageText: 'USB ストレージ 35% 使用中',

        initApp() {
            this.loadDashboard();
            this.loadFiles();
            this.loadLocks();
            this.loadCommits();
            this.loadReleases();
            this.connectWS();
            // Refresh every 10s
            setInterval(() => {
                if (this.page === 'files') this.loadFiles();
                if (this.page === 'locks') this.loadLocks();
                if (this.page === 'dashboard') this.loadDashboard();
            }, 10000);
        },

        async loadDashboard() {
            const res = await fetch('/dashboard-stats');
            if (res.ok) this.dashboard = await res.json();
        },

        async loadFiles() {
            const res = await fetch(`/api/files?repo=${encodeURIComponent(this.repo)}&prefix=${encodeURIComponent(this.currentPath)}`);
            if (res.ok) this.files = await res.json();
        },

        async loadLocks() {
            const res = await fetch('/api/locks');
            if (res.ok) this.locks = await res.json();
        },

        async loadCommits() {
            const res = await fetch(`/api/commits?repo=${encodeURIComponent(this.repo)}`);
            if (res.ok) this.commits = await res.json();
        },

        async loadReleases() {
            const res = await fetch(`/api/releases?repo=${encodeURIComponent(this.repo)}`);
            if (res.ok) this.releases = await res.json();
        },

        async doSearch() {
            if (!this.query) return;
            const res = await fetch(`/api/search?q=${encodeURIComponent(this.query)}`);
            if (res.ok) {
                const data = await res.json();
                this.searchResults = data.hits || [];
                this.page = 'search';
            }
        },

        async openFile(f) {
            this.selectedFile = f;
            const res = await fetch(`/api/file?repo=${encodeURIComponent(f.repo)}&path=${encodeURIComponent(f.path)}`);
            if (res.ok) this.fileDetail = await res.json();
        },

        async toggleLock(f) {
            const lock = !f.locked_by;
            const res = await fetch('/api/lock', {
                method: lock ? 'POST' : 'DELETE',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ repo: f.repo, path: f.path, user: this.user })
            });
            if (res.ok) {
                f.locked_by = lock ? this.user : '';
                this.loadLocks();
            }
        },

        async forceUnlock(f) {
            await fetch('/api/lock', {
                method: 'DELETE',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ repo: f.repo, path: f.path, user: this.user })
            });
            this.loadLocks();
            this.loadFiles();
        },

        async createRelease() {
            await fetch(`/api/releases?repo=${encodeURIComponent(this.repo)}`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(this.newRelease)
            });
            this.showRelease = false;
            this.newRelease = { tag: '', commit: '', note: '' };
            this.loadReleases();
        },

        saveSettings() {
            localStorage.setItem('lolit:repo', this.repo);
            localStorage.setItem('lolit:user', this.user);
            this.showSettings = false;
            this.loadFiles();
            this.loadCommits();
            this.loadReleases();
        },

        connectWS() {
            const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
            const ws = new WebSocket(`${proto}//${location.host}/ws`);
            ws.onmessage = (ev) => {
                const data = JSON.parse(ev.data);
                if (data.type === 'lock') {
                    this.loadLocks();
                    this.loadFiles();
                }
            };
            ws.onclose = () => setTimeout(() => this.connectWS(), 3000);
        },

        iconFor(type) {
            const map = {
                sldprt: '■', sldasm: '▣', kicad_pcb: '●',
                kicad_sch: '◎', step: '◈', stl: '◆', other: '▤'
            };
            return map[type] || '▤';
        },

        iconClass(type) {
            if (type === 'sldprt' || type === 'sldasm') return 'icon-blue';
            if (type === 'kicad_pcb' || type === 'kicad_sch') return 'icon-green';
            if (type === 'step' || type === 'stl') return 'icon-orange';
            if (type === 'other') return 'icon-gray';
            return 'icon-gray';
        },

        formatTime(ts) {
            if (!ts) return '-';
            return new Date(ts * 1000).toLocaleString('ja-JP');
        }
    };
}
