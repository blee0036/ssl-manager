// === Domains Page Logic ===
document.addEventListener('DOMContentLoaded', () => {
    if (!App.requireAuth()) return;

    loadDomains();

    document.getElementById('btn-add-domain').addEventListener('click', showAddModal);
    document.getElementById('domain-search').addEventListener('input', debounce(() => { loadDomains(); }, 300));

    async function loadDomains() {
        const search = document.getElementById('domain-search').value.trim();
        let url = `/api/domains`;
        const params = [];
        if (search) params.push(`name=${encodeURIComponent(search)}`);
        if (params.length > 0) url += '?' + params.join('&');

        try {
            const data = await App.get(url);
            const result = data.data || data;
            const domains = Array.isArray(result) ? result : [];

            renderDomains(domains);
        } catch (e) {
            App.toast('加载域名列表失败: ' + e.message, 'error');
        }
    }

    function renderDomains(domains) {
        const tbody = document.getElementById('domains-body');
        if (domains.length === 0) {
            tbody.innerHTML = '<tr><td colspan="7" class="empty-state">暂无域名</td></tr>';
            return;
        }
        tbody.innerHTML = domains.map(d => {
            const mr = d.latest_monitor_result;
            let tlsStatus = '-';
            let certMatch = '-';
            let expireAt = '-';
            let lastCheck = '-';

            if (mr) {
                tlsStatus = mr.tls_success ?
                    '<span class="badge badge-success">正常</span>' :
                    '<span class="badge badge-danger">失败</span>';
                certMatch = mr.domain_matched ?
                    '<span class="badge badge-success">匹配</span>' :
                    '<span class="badge badge-warning">不匹配</span>';
                if (mr.expire_at) {
                    const days = mr.days_remaining !== null && mr.days_remaining !== undefined ? mr.days_remaining : '';
                    const daysText = days !== '' ? ` (${days}天)` : '';
                    expireAt = App.formatDate(mr.expire_at) + daysText;
                }
                lastCheck = App.formatDate(mr.checked_at);
                if (mr.error_message) {
                    tlsStatus = `<span class="badge badge-danger" title="${App.escapeHtml(mr.error_message)}">错误</span>`;
                }
            }

            return `
                <tr>
                    <td><strong>${App.escapeHtml(d.name)}</strong></td>
                    <td>${d.monitor_port || 443}</td>
                    <td>${tlsStatus}</td>
                    <td>${certMatch}</td>
                    <td>${expireAt}</td>
                    <td>${lastCheck}</td>
                    <td>
                        ${App._currentRole !== 'readonly' ? `<button class="btn btn-sm btn-secondary" onclick="probeDomain('${d.id}')">探测</button>` : ''}
                        <button class="btn btn-sm btn-secondary" onclick="viewDomain('${d.id}')">详情</button>
                        ${App._currentRole !== 'readonly' ? `<button class="btn btn-sm btn-danger" onclick="deleteDomain('${d.id}')">删除</button>` : ''}
                    </td>
                </tr>
            `;
        }).join('');
    }

    window.probeDomain = async function(id) {
        try {
            await App.post(`/api/domains/${id}/probe`, {});
            App.toast('探测已触发', 'success');
            setTimeout(loadDomains, 2000);
        } catch (e) {
            App.toast('探测失败: ' + e.message, 'error');
        }
    };

    window.viewDomain = async function(id) {
        try {
            const data = await App.get(`/api/domains/${id}`);
            const d = data.data || data;
            const mr = d.latest_monitor_result;

            let monitorHtml = '';
            if (mr) {
                const tlsStatus = mr.tls_success ?
                    '<span class="badge badge-success">正常</span>' :
                    '<span class="badge badge-danger">失败</span>';
                const matchStatus = mr.domain_matched ?
                    '<span class="badge badge-success">匹配</span>' :
                    '<span class="badge badge-warning">不匹配</span>';
                const chainValid = mr.chain_valid ?
                    '<span class="badge badge-success">有效</span>' :
                    '<span class="badge badge-warning">无效</span>';

                monitorHtml = `
                    <h4 style="margin-top:16px;">最新监控结果</h4>
                    <div class="form-group"><label>TLS 状态</label><p>${tlsStatus}</p></div>
                    <div class="form-group"><label>域名匹配</label><p>${matchStatus}</p></div>
                    <div class="form-group"><label>证书链</label><p>${chainValid}</p></div>
                    <div class="form-group"><label>签发者</label><p>${App.escapeHtml(mr.issuer || '-')}</p></div>
                    <div class="form-group"><label>证书指纹</label><p style="word-break:break-all;font-size:12px;">${App.escapeHtml(mr.certificate_fingerprint_sha256 || '-')}</p></div>
                    <div class="form-group"><label>到期时间</label><p>${mr.expire_at ? App.formatDate(mr.expire_at) + (mr.days_remaining !== null ? ' (' + mr.days_remaining + '天)' : '') : '-'}</p></div>
                    <div class="form-group"><label>检查时间</label><p>${App.formatDate(mr.checked_at)}</p></div>
                    ${mr.error_message ? '<div class="form-group"><label>错误信息</label><p class="text-danger">' + App.escapeHtml(mr.error_message) + '</p></div>' : ''}
                `;
            } else {
                monitorHtml = '<p style="margin-top:16px;color:#999;">暂无监控结果，请点击"探测"按钮</p>';
            }

            const html = `
                <div class="form-group"><label>域名</label><p>${App.escapeHtml(d.name)}</p></div>
                <div class="form-group"><label>监控端口</label><p>${d.monitor_port || 443}</p></div>
                <div class="form-group"><label>监控状态</label><p>${d.monitor_enabled ? '<span class="badge badge-success">启用</span>' : '<span class="badge badge-gray">禁用</span>'}</p></div>
                <div class="form-group"><label>来源</label><p>${App.escapeHtml(d.source || '-')}</p></div>
                <div class="form-group"><label>关联证书 ID</label><p>${App.escapeHtml(d.linked_certificate_id || '-')}</p></div>
                <div class="form-group"><label>创建时间</label><p>${App.formatDate(d.created_at)}</p></div>
                <div class="form-group"><label>更新时间</label><p>${App.formatDate(d.updated_at)}</p></div>
                ${monitorHtml}
            `;
            App.showModal('域名详情', html);
        } catch (e) {
            App.toast('加载详情失败: ' + e.message, 'error');
        }
    };

    window.deleteDomain = async function(id) {
        if (!App.confirm('确定要删除此域名监控吗？')) return;
        try {
            await App.delete(`/api/domains/${id}`);
            App.toast('域名已删除', 'success');
            loadDomains();
        } catch (e) {
            App.toast('删除失败: ' + e.message, 'error');
        }
    };

    function showAddModal() {
        const html = `
            <form id="add-domain-form">
                <div class="form-group">
                    <label>域名（支持批量，每行一个，或用逗号分隔）</label>
                    <textarea id="add-domain-names" required placeholder="example.com&#10;sub.example.com&#10;another.com" rows="5" style="width:100%;font-family:monospace;"></textarea>
                </div>
                <div class="form-group">
                    <label>监控端口</label>
                    <input type="number" id="add-domain-port" value="443" min="1" max="65535">
                </div>
                <div class="form-group">
                    <label>关联证书 ID（可选）</label>
                    <input type="text" id="add-domain-cert-id" placeholder="留空则不关联">
                </div>
            </form>
        `;
        const footer = `
            <button class="btn btn-secondary" onclick="App.closeModal()">取消</button>
            <button class="btn btn-primary" onclick="submitAddDomain()">添加</button>
        `;
        App.showModal('添加域名监控', html, footer);
    }

    window.submitAddDomain = async function() {
        const raw = document.getElementById('add-domain-names').value.trim();
        const monitor_port = parseInt(document.getElementById('add-domain-port').value) || 443;
        const certId = document.getElementById('add-domain-cert-id').value.trim();

        if (!raw) {
            App.toast('请输入域名', 'warning');
            return;
        }

        // Split by newline, comma (English/Chinese), and filter empty/whitespace entries
        const names = raw.split(/[\n,，]/)
            .map(s => s.trim())
            .filter(s => s.length > 0);

        if (names.length === 0) {
            App.toast('请输入至少一个有效域名', 'warning');
            return;
        }

        // Remove duplicates
        const uniqueNames = [...new Set(names)];

        let successCount = 0;
        let failCount = 0;
        const errors = [];

        for (const name of uniqueNames) {
            const body = { name: name, monitor_port: monitor_port };
            if (certId) body.linked_certificate_id = certId;

            try {
                await App.post('/api/domains', body);
                successCount++;
            } catch (e) {
                failCount++;
                errors.push(`${name}: ${e.message}`);
            }
        }

        App.closeModal();

        if (failCount === 0) {
            App.toast(`成功添加 ${successCount} 个域名`, 'success');
        } else if (successCount === 0) {
            App.toast(`全部失败（${failCount} 个）: ${errors[0]}`, 'error');
        } else {
            App.toast(`添加完成：${successCount} 成功，${failCount} 失败`, 'warning');
        }

        loadDomains();
    };

    function debounce(fn, delay) {
        let timer;
        return function(...args) { clearTimeout(timer); timer = setTimeout(() => fn.apply(this, args), delay); };
    }
});
