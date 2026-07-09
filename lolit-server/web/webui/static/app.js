function lolit() {
    return {
        // Auth
        authed: false,
        mode: 'login', // 'login' | 'register'
        loginForm: { username: '', password: '', display_name: '' },
        loginError: '',
        currentUser: {},
        token: localStorage.getItem('lolit:token') || '',

        // Repos / navigation
        repos: [],
        repoFilter: '',
        repo: localStorage.getItem('lolit:repo') || '',
        tab: 'code',
        currentPath: '',
        files: [],
        commits: [],
        locks: [],
        releases: [],
        members: [],
        query: '',
        searchResults: [],

        selectedFile: null,
        fileDetail: {},
        fileHistory: [],

        // Upload
        dragOver: false,
        pendingUploads: [], // [{path, file}]
        uploadMessage: '',
        uploadError: '',
        uploading: false,
        showUploadConfirm: false,

        // Modals
        showRelease: false,
        newRelease: { tag: '', commit: '', note: '' },
        showInvite: false,
        inviteForm: { username: '', display_name: '', password: '' },
        inviteError: '',

        async initApp() {
            if (!this.token) { this.authed = false; return; }
            const me = await this.api('/api/auth/me');
            if (!me) { this.logoutLocal(); return; }
            this.currentUser = me;
            this.authed = true;
            await this.loadRepos();
            if (this.repo) await this.selectRepo(this.repo);
            this.connectWS();
        },

        // ---------- auth ----------
        async doLogin() {
            this.loginError = '';
            try {
                const res = await fetch('/api/auth/login', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ username: this.loginForm.username, password: this.loginForm.password })
                });
                const data = await res.json();
                if (!res.ok) { this.loginError = data.error || 'ログインに失敗しました'; return; }
                this.token = data.token;
                localStorage.setItem('lolit:token', this.token);
                this.currentUser = data.user;
                this.authed = true;
                await this.loadRepos();
                this.connectWS();
            } catch (e) {
                this.loginError = 'サーバーに接続できませんでした';
            }
        },

        async doRegister() {
            this.loginError = '';
            if (!this.loginForm.username || this.loginForm.password.length < 8) {
                this.loginError = 'ユーザー名とパスワード（8文字以上）を入力してください';
                return;
            }
            try {
                const res = await fetch('/api/auth/register', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(this.loginForm)
                });
                const data = await res.json();
                if (!res.ok) { this.loginError = data.error || '作成に失敗しました'; return; }
                // Log in immediately with the same credentials.
                await this.doLogin();
            } catch (e) {
                this.loginError = 'サーバーに接続できませんでした';
            }
        },

        async doLogout() {
            await fetch('/api/auth/logout', { method: 'POST' });
            this.logoutLocal();
        },

        logoutLocal() {
            localStorage.removeItem('lolit:token');
            this.token = '';
            this.authed = false;
            this.currentUser = {};
        },

        // Thin fetch wrapper: attaches the bearer token, and logs out on 401.
        async api(path, opts) {
            opts = opts || {};
            opts.headers = Object.assign({}, opts.headers, { Authorization: 'Bearer ' + this.token });
            const res = await fetch(path, opts);
            if (res.status === 401) { this.logoutLocal(); return null; }
            if (!res.ok) return null;
            const ct = res.headers.get('content-type') || '';
            return ct.includes('application/json') ? res.json() : res.text();
        },

        // ---------- repos ----------
        async loadRepos() {
            this.repos = (await this.api('/api/repos')) || [];
        },

        filteredRepos() {
            if (!this.repoFilter) return this.repos;
            const q = this.repoFilter.toLowerCase();
            return this.repos.filter(r => r.toLowerCase().includes(q));
        },

        repoOwner(r) { return (r || '').split('/')[0] || ''; },
        repoName(r) { const p = (r || '').split('/'); return p[p.length - 1]; },

        async selectRepo(r) {
            this.repo = r;
            this.currentPath = '';
            this.selectedFile = null;
            localStorage.setItem('lolit:repo', r);
            this.tab = 'code';
            await Promise.all([this.loadFiles(), this.loadCommits(), this.loadLocks(), this.loadReleases()]);
            if (this.currentUser.role === 'admin') await this.loadMembers();
        },

        switchTab(t) { this.tab = t; },

        async loadFiles() {
            if (!this.repo) return;
            this.files = (await this.api(`/api/files?repo=${encodeURIComponent(this.repo)}`)) || [];
        },
        async loadCommits() {
            if (!this.repo) return;
            this.commits = (await this.api(`/api/commits?repo=${encodeURIComponent(this.repo)}`)) || [];
        },
        async loadLocks() {
            if (!this.repo) return;
            this.locks = (await this.api(`/api/locks?repo=${encodeURIComponent(this.repo)}`)) || [];
        },
        async loadReleases() {
            if (!this.repo) return;
            this.releases = (await this.api(`/api/releases?repo=${encodeURIComponent(this.repo)}`)) || [];
        },
        async loadMembers() {
            this.members = (await this.api('/api/users')) || [];
        },
        repoLocks() { return this.locks; },

        // ---------- file tree (built client-side from the flat file list) ----------
        pathSegments() { return this.currentPath ? this.currentPath.split('/') : []; },
        pathUpTo(i) { return this.pathSegments().slice(0, i + 1).join('/'); },
        navigateTo(path) { this.currentPath = path; this.selectedFile = null; },

        currentTree() {
            const prefix = this.currentPath ? this.currentPath + '/' : '';
            const dirs = new Map();
            const rows = [];
            for (const f of this.files) {
                if (!f.path.startsWith(prefix)) continue;
                const rest = f.path.slice(prefix.length);
                if (!rest) continue;
                const slash = rest.indexOf('/');
                if (slash === -1) {
                    rows.push({ key: f.path, name: rest, isDir: false, file: f });
                } else {
                    const dirName = rest.slice(0, slash);
                    if (!dirs.has(dirName)) {
                        dirs.set(dirName, true);
                        rows.push({ key: 'dir:' + dirName, name: dirName, isDir: true, path: prefix + dirName });
                    }
                }
            }
            rows.sort((a, b) => (a.isDir === b.isDir) ? a.name.localeCompare(b.name) : (a.isDir ? -1 : 1));
            return rows;
        },

        async openFile(f) {
            this.selectedFile = f;
            this.fileDetail = (await this.api(`/api/file?repo=${encodeURIComponent(f.repo)}&path=${encodeURIComponent(f.path)}`)) || {};
            this.fileHistory = (await this.api(`/api/history?repo=${encodeURIComponent(f.repo)}&path=${encodeURIComponent(f.path)}`)) || [];
        },

        parsedKicadDiff() {
            if (!this.fileDetail || !this.fileDetail.kicad_diff) return null;
            try { return JSON.parse(this.fileDetail.kicad_diff); } catch (e) { return null; }
        },

        async toggleLock(f) {
            const lock = !f.locked_by;
            const res = await this.api('/api/lock', {
                method: lock ? 'POST' : 'DELETE',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ repo: f.repo, path: f.path, user: this.currentUser.username })
            });
            if (res) {
                f.locked_by = lock ? this.currentUser.username : '';
                this.loadLocks();
            }
        },

        async forceUnlock(f) {
            await this.api('/api/lock', {
                method: 'DELETE',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ repo: this.repo, path: f.path, user: this.currentUser.username })
            });
            this.loadLocks();
            this.loadFiles();
        },

        // ---------- upload (drag & drop, or plain file picker) ----------
        async onDrop(ev) {
            this.dragOver = false;
            const items = ev.dataTransfer.items;
            let entries = [];
            if (items && items.length && items[0].webkitGetAsEntry) {
                for (const item of items) {
                    const entry = item.webkitGetAsEntry && item.webkitGetAsEntry();
                    if (entry) entries.push(entry);
                }
                const collected = [];
                for (const entry of entries) await this.walkEntry(entry, '', collected);
                this.stageUploads(collected);
            } else {
                this.stageUploads(Array.from(ev.dataTransfer.files).map(f => ({ file: f, path: f.name })));
            }
        },

        async walkEntry(entry, prefix, out) {
            if (entry.isFile) {
                await new Promise(resolve => entry.file(file => { out.push({ file, path: prefix + entry.name }); resolve(); }));
            } else if (entry.isDirectory) {
                const reader = entry.createReader();
                const children = await new Promise(resolve => reader.readEntries(resolve));
                for (const child of children) await this.walkEntry(child, prefix + entry.name + '/', out);
            }
        },

        onFilePick(ev) {
            const files = Array.from(ev.target.files);
            this.stageUploads(files.map(f => ({ file: f, path: f.webkitRelativePath || f.name })));
            ev.target.value = '';
        },

        stageUploads(items) {
            if (!items.length) return;
            const prefix = this.currentPath ? this.currentPath + '/' : '';
            this.pendingUploads = items.map(it => ({ file: it.file, path: prefix + it.path }));
            this.uploadMessage = '';
            this.uploadError = '';
            this.showUploadConfirm = true;
        },

        cancelUpload() {
            this.showUploadConfirm = false;
            this.pendingUploads = [];
            this.uploadError = '';
        },

        async confirmUpload() {
            if (!this.pendingUploads.length) { this.cancelUpload(); return; }
            this.uploading = true;
            this.uploadError = '';
            const form = new FormData();
            form.append('repo', this.repo);
            form.append('message', this.uploadMessage);
            for (const u of this.pendingUploads) {
                form.append('files', u.file);
                form.append('paths', u.path);
            }
            try {
                const res = await fetch('/api/upload', {
                    method: 'POST',
                    headers: { Authorization: 'Bearer ' + this.token },
                    body: form
                });
                const data = await res.json();
                if (!res.ok) { this.uploadError = data.error || 'アップロードに失敗しました'; this.uploading = false; return; }
                this.uploading = false;
                this.showUploadConfirm = false;
                this.pendingUploads = [];
                await Promise.all([this.loadFiles(), this.loadCommits()]);
            } catch (e) {
                this.uploadError = 'サーバーに接続できませんでした';
                this.uploading = false;
            }
        },

        // ---------- search ----------
        async doSearch() {
            if (!this.query) return;
            const data = await this.api(`/api/search?q=${encodeURIComponent(this.query)}`);
            this.searchResults = (data && data.hits) || [];
            this.tab = 'search';
        },

        // ---------- releases ----------
        async createRelease() {
            await this.api(`/api/releases?repo=${encodeURIComponent(this.repo)}`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(this.newRelease)
            });
            this.showRelease = false;
            this.newRelease = { tag: '', commit: '', note: '' };
            this.loadReleases();
        },

        // ---------- members (admin) ----------
        async createMember() {
            this.inviteError = '';
            const data = await this.api('/api/auth/register', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(this.inviteForm)
            });
            if (!data) { this.inviteError = '追加に失敗しました（ユーザー名が重複していないか確認してください）'; return; }
            this.showInvite = false;
            this.inviteForm = { username: '', display_name: '', password: '' };
            this.loadMembers();
        },

        async changeRole(m) {
            await this.api('/api/users', {
                method: 'PATCH',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ id: m.id, role: m.role })
            });
        },

        async removeMember(m) {
            await this.api(`/api/users?id=${m.id}`, { method: 'DELETE' });
            this.loadMembers();
        },

        // ---------- websocket (live lock updates) ----------
        connectWS() {
            const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
            const ws = new WebSocket(`${proto}//${location.host}/ws`);
            ws.onmessage = (ev) => {
                const data = JSON.parse(ev.data);
                if (data.type === 'lock' && data.repo === this.repo) {
                    this.loadLocks();
                    this.loadFiles();
                }
            };
            ws.onclose = () => { if (this.authed) setTimeout(() => this.connectWS(), 3000); };
        },

        // ---------- display helpers ----------
        initials(name) {
            if (!name) return '?';
            return name.trim().slice(0, 2).toUpperCase();
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
            return 'icon-gray';
        },

        formatTime(ts) {
            if (!ts) return '-';
            return new Date(ts * 1000).toLocaleString('ja-JP');
        }
    };
}
