// app.js - Common utilities and API client for SSL Manager
const App = {
    // --- HTTP helpers ---
    _headers() {
        const headers = { 'Content-Type': 'application/json' };
        const token = localStorage.getItem('token');
        if (token) {
            headers['Authorization'] = 'Bearer ' + token;
        }
        return headers;
    },

    async _request(method, url, body) {
        const opts = {
            method: method,
            headers: this._headers(),
        };
        if (body !== undefined && body !== null) {
            opts.body = JSON.stringify(body);
        }
        const resp = await fetch(url, opts);
        const json = await resp.json();
        if (!resp.ok) {
            // Auto-redirect to login on 401 for protected APIs only.
            // Don't redirect for login/init endpoints — those return 401 as normal business errors.
            if (resp.status === 401 && !url.startsWith('/api/auth/') && !url.startsWith('/init')) {
                localStorage.removeItem('token');
                window.location.href = '/login';
                // Return a never-resolving promise to prevent callers from continuing
                return new Promise(() => {});
            }
            const err = new Error(json.message || 'Request failed');
            err.code = json.code || resp.status;
            err.detail = json.detail || '';
            throw err;
        }
        return json;
    },

    async get(url) {
        return this._request('GET', url);
    },

    async post(url, body) {
        return this._request('POST', url, body);
    },

    async put(url, body) {
        return this._request('PUT', url, body);
    },

    async delete(url) {
        return this._request('DELETE', url);
    },

    // --- Auth ---
    requireAuth() {
        const token = localStorage.getItem('token');
        if (!token) {
            window.location.href = '/login';
            return false;
        }
        return true;
    },

    logout() {
        localStorage.removeItem('token');
        window.location.href = '/login';
    },

    // --- Toast notifications ---
    toast(msg, type) {
        type = type || 'info';
        const container = document.getElementById('toast-container') || this._createToastContainer();
        const toast = document.createElement('div');
        toast.className = 'toast toast-' + type;
        toast.textContent = msg;
        container.appendChild(toast);
        setTimeout(() => {
            toast.classList.add('toast-fade');
            setTimeout(() => toast.remove(), 300);
        }, 3000);
    },

    _createToastContainer() {
        const c = document.createElement('div');
        c.id = 'toast-container';
        c.style.cssText = 'position:fixed;top:20px;right:20px;z-index:9999;';
        document.body.appendChild(c);
        return c;
    },

    // --- Modal ---
    showModal(title, html, footer) {
        let overlay = document.getElementById('app-modal-overlay');
        if (!overlay) {
            overlay = document.createElement('div');
            overlay.id = 'app-modal-overlay';
            overlay.style.cssText = 'position:fixed;top:0;left:0;width:100%;height:100%;background:rgba(0,0,0,0.5);z-index:10000;display:flex;align-items:center;justify-content:center;';
            document.body.appendChild(overlay);
        }
        overlay.innerHTML = `
            <div class="modal-dialog" style="background:#fff;border-radius:8px;padding:24px;min-width:400px;max-width:600px;max-height:80vh;overflow-y:auto;">
                <div class="modal-header" style="display:flex;justify-content:space-between;align-items:center;margin-bottom:16px;">
                    <h3 style="margin:0;">${this.escapeHtml(title)}</h3>
                    <button onclick="App.closeModal()" style="border:none;background:none;font-size:20px;cursor:pointer;">&times;</button>
                </div>
                <div class="modal-body">${html}</div>
                ${footer ? '<div class="modal-footer" style="margin-top:16px;text-align:right;">' + footer + '</div>' : ''}
            </div>
        `;
        overlay.style.display = 'flex';
    },

    closeModal() {
        const overlay = document.getElementById('app-modal-overlay');
        if (overlay) {
            overlay.style.display = 'none';
            overlay.innerHTML = '';
        }
    },

    // --- Utilities ---
    formatDate(isoString) {
        if (!isoString) return '-';
        const d = new Date(isoString);
        if (isNaN(d.getTime())) return '-';
        return d.toLocaleString();
    },

    escapeHtml(str) {
        if (!str) return '';
        const div = document.createElement('div');
        div.textContent = str;
        return div.innerHTML;
    },

    daysUntil(isoDate) {
        if (!isoDate) return null;
        const target = new Date(isoDate);
        const now = new Date();
        const diff = target.getTime() - now.getTime();
        return Math.ceil(diff / (1000 * 60 * 60 * 24));
    },

    // --- Confirm dialog ---
    confirm(msg) {
        return window.confirm(msg);
    },

    // --- Tab controller ---
    // Handles .tab-button[data-tab] or .tab[data-tab] clicks:
    // toggles .active on tab buttons and corresponding .tab-content elements,
    // then dispatches a 'tabChanged' CustomEvent with detail.tabId.
    initTabs() {
        document.addEventListener('click', (e) => {
            const tabBtn = e.target.closest('[data-tab]');
            if (!tabBtn) return;

            const tabId = tabBtn.getAttribute('data-tab');
            const group = tabBtn.closest('.tab-group');
            if (!group) return;

            // Deactivate all tabs in this group
            group.querySelectorAll('[data-tab]').forEach(btn => btn.classList.remove('active'));
            tabBtn.classList.add('active');

            // Find the parent container that holds both the tab-group and tab-content elements
            const container = group.parentElement;
            if (!container) return;

            // Hide all tab-content in this container, show the target
            container.querySelectorAll('.tab-content').forEach(content => {
                content.classList.remove('active');
                content.style.display = 'none';
            });

            const targetContent = container.querySelector('#' + tabId) || document.getElementById(tabId);
            if (targetContent) {
                targetContent.classList.add('active');
                targetContent.style.display = '';
            }

            // Dispatch event for listeners (e.g., lazy-loading alert history)
            document.dispatchEvent(new CustomEvent('tabChanged', { detail: { tabId: tabId } }));
        });
    },

    // --- Pagination renderer ---
    renderPagination(containerId, currentPage, totalPages, onPageChange) {
        const container = document.getElementById(containerId);
        if (!container) return;

        if (totalPages <= 1) {
            container.innerHTML = '';
            return;
        }

        let html = '';
        // Previous button
        if (currentPage > 1) {
            html += `<button class="btn btn-sm btn-secondary" data-page="${currentPage - 1}">&laquo; 上一页</button> `;
        }

        // Page numbers (show max 7 pages around current)
        const start = Math.max(1, currentPage - 3);
        const end = Math.min(totalPages, currentPage + 3);

        if (start > 1) {
            html += `<button class="btn btn-sm btn-secondary" data-page="1">1</button> `;
            if (start > 2) html += '<span>...</span> ';
        }

        for (let i = start; i <= end; i++) {
            if (i === currentPage) {
                html += `<button class="btn btn-sm btn-primary" disabled>${i}</button> `;
            } else {
                html += `<button class="btn btn-sm btn-secondary" data-page="${i}">${i}</button> `;
            }
        }

        if (end < totalPages) {
            if (end < totalPages - 1) html += '<span>...</span> ';
            html += `<button class="btn btn-sm btn-secondary" data-page="${totalPages}">${totalPages}</button> `;
        }

        // Next button
        if (currentPage < totalPages) {
            html += `<button class="btn btn-sm btn-secondary" data-page="${currentPage + 1}">下一页 &raquo;</button>`;
        }

        container.innerHTML = html;

        // Bind click events
        container.querySelectorAll('[data-page]').forEach(btn => {
            btn.addEventListener('click', () => {
                const page = parseInt(btn.getAttribute('data-page'));
                if (onPageChange) onPageChange(page);
            });
        });
    }
};

// Initialize on DOM ready
document.addEventListener('DOMContentLoaded', () => {
    App.initTabs();

    // Bind logout button
    const logoutBtn = document.getElementById('logout-btn');
    if (logoutBtn) {
        logoutBtn.addEventListener('click', () => App.logout());
    }

    // Role-based UI adjustments
    const token = localStorage.getItem('token');
    if (token) {
        try {
            // Decode JWT payload (base64url) to get role
            const payload = JSON.parse(atob(token.split('.')[1].replace(/-/g, '+').replace(/_/g, '/')));
            const role = payload.role || '';

            // Store role globally for page-level checks
            App._currentRole = role;

            if (role !== 'admin') {
                document.querySelectorAll('.admin-only').forEach(el => {
                    el.style.display = 'none';
                });
            }

            // Readonly users: hide all write-action buttons and nav items they can't access
            if (role === 'readonly') {
                // Hide toolbar buttons (add, sync, etc.) — these are write operations
                document.querySelectorAll('.toolbar .btn-primary, .toolbar .btn-secondary').forEach(el => {
                    el.style.display = 'none';
                });
                // Hide nav items for pages readonly can't access at all
                document.querySelectorAll('.nav-item[data-page="system"]').forEach(el => {
                    el.style.display = 'none';
                });
                document.querySelectorAll('.nav-item[data-page="thirdpart-dns"]').forEach(el => {
                    el.style.display = 'none';
                });
                // Add a body class so page-specific JS can check
                document.body.classList.add('readonly-mode');
            }

            // Display current username in sidebar
            const userEl = document.getElementById('current-user');
            if (userEl && payload.username) {
                userEl.textContent = payload.username + (role === 'readonly' ? ' (只读)' : '');
            }
        } catch (e) {
            // If token can't be decoded, ignore — auth will fail on API call
        }
    }
});
