// === Third-party DNS Page Logic ===
document.addEventListener('DOMContentLoaded', () => {
    if (!App.requireAuth()) return;

    loadDNSConfigs();

    document.getElementById('btn-add-dns').addEventListener('click', showAddModal);

    async function loadDNSConfigs() {
        try {
            const data = await App.get('/api/thirdpart-dns');
            const result = data.data || data;
            const configs = Array.isArray(result) ? result : (result.items || []);
            renderConfigs(configs);
        } catch (e) {
            App.toast('加载 DNS 配置失败: ' + e.message, 'error');
        }
    }

    function renderConfigs(configs) {
        const tbody = document.getElementById('dns-body');
        if (configs.length === 0) {
            tbody.innerHTML = '<tr><td colspan="5" class="empty-state">暂无 DNS 配置</td></tr>';
            return;
        }
        tbody.innerHTML = configs.map(c => {
            const statusBadge = c.enabled ?
                '<span class="badge badge-success">启用</span>' :
                '<span class="badge badge-gray">禁用</span>';
            const domains = Array.isArray(c.main_domains) ? c.main_domains.join(', ') : '-';
            return `
                <tr>
                    <td><strong>${App.escapeHtml(c.name || '-')}</strong></td>
                    <td>${App.escapeHtml(c.type || '-')}</td>
                    <td>${App.escapeHtml(domains)}</td>
                    <td>${statusBadge}</td>
                    <td>
                        <button class="btn btn-sm btn-secondary" onclick="syncDNS('${c.id}')">同步</button>
                        <button class="btn btn-sm btn-secondary" onclick="editDNS('${c.id}')">编辑</button>
                        <button class="btn btn-sm btn-danger" onclick="deleteDNS('${c.id}')">删除</button>
                    </td>
                </tr>
            `;
        }).join('');
    }

    window.syncDNS = async function(id) {
        try {
            await App.post(`/api/thirdpart-dns/${id}/sync`, {});
            App.toast('同步已触发', 'success');
            setTimeout(loadDNSConfigs, 2000);
        } catch (e) {
            App.toast('同步失败: ' + e.message, 'error');
        }
    };

    window.editDNS = async function(id) {
        try {
            const data = await App.get(`/api/thirdpart-dns/${id}`);
            const c = data.data || data;
            showAddModal(c);
        } catch (e) {
            App.toast('加载配置失败: ' + e.message, 'error');
        }
    };

    window.deleteDNS = async function(id) {
        if (!App.confirm('确定要删除此 DNS 配置吗？')) return;
        try {
            await App.delete(`/api/thirdpart-dns/${id}`);
            App.toast('DNS 配置已删除', 'success');
            loadDNSConfigs();
        } catch (e) {
            App.toast('删除失败: ' + e.message, 'error');
        }
    };

    function showAddModal(existing) {
        const isEdit = !!existing;
        const existingConfigJson = existing?.config_json || '{}';
        const existingMainDomains = Array.isArray(existing?.main_domains) ? existing.main_domains.join(', ') : '';
        const html = `
            <form id="dns-form">
                <div class="form-group">
                    <label>名称</label>
                    <input type="text" id="dns-name" required placeholder="My Cloudflare" value="${App.escapeHtml(existing?.name || '')}">
                </div>
                <div class="form-group">
                    <label>类型</label>
                    <select id="dns-type">
                        <option value="cloudflare" ${(!existing || existing.type === 'cloudflare') ? 'selected' : ''}>Cloudflare</option>
                    </select>
                </div>
                <div class="form-group">
                    <label>主域名（多个用逗号分隔，留空则获取所有区域）</label>
                    <input type="text" id="dns-main-domains" placeholder="example.com, example.org" value="${App.escapeHtml(existingMainDomains)}">
                </div>
                <div class="form-group">
                    <label>API Token</label>
                    <input type="password" id="dns-api-token" ${isEdit ? '' : 'required'} placeholder="${isEdit ? '留空则不修改' : 'API Token'}">
                </div>
                <div class="form-group">
                    <label>配置 JSON（供应商特定配置）</label>
                    <textarea id="dns-config-json" rows="3" placeholder='{"zone_id": "..."}'>${App.escapeHtml(existingConfigJson)}</textarea>
                </div>
            </form>
        `;
        const footer = `
            <button class="btn btn-secondary" onclick="App.closeModal()">取消</button>
            <button class="btn btn-primary" onclick="submitDNS('${isEdit ? existing.id : ''}')">${isEdit ? '保存' : '添加'}</button>
        `;
        App.showModal(isEdit ? '编辑 DNS 配置' : '添加 DNS 配置', html, footer);
    }

    window.submitDNS = async function(id) {
        const name = document.getElementById('dns-name').value.trim();
        const type = document.getElementById('dns-type').value;
        const mainDomainsStr = document.getElementById('dns-main-domains').value.trim();
        const apiToken = document.getElementById('dns-api-token').value.trim();
        const configJsonStr = document.getElementById('dns-config-json').value.trim();

        if (!name) {
            App.toast('请填写名称', 'warning');
            return;
        }

        const main_domains = mainDomainsStr ? mainDomainsStr.split(',').map(s => s.trim()).filter(s => s) : [];
        const config_json = configJsonStr || '{}';

        const body = { name, type, config_json, main_domains };
        if (apiToken) body.api_token = apiToken;

        try {
            if (id) {
                await App.put(`/api/thirdpart-dns/${id}`, body);
            } else {
                await App.post('/api/thirdpart-dns', body);
            }
            App.toast(id ? 'DNS 配置已更新' : 'DNS 配置已添加', 'success');
            App.closeModal();
            loadDNSConfigs();
        } catch (e) {
            App.toast('操作失败: ' + e.message, 'error');
        }
    };
});
