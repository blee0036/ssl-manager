// === Alerts Page Logic ===
document.addEventListener('DOMContentLoaded', () => {
    if (!App.requireAuth()) return;

    let channelsList = [];

    loadChannels();

    document.getElementById('btn-add-channel').addEventListener('click', () => showAddChannelModal());

    document.addEventListener('tabChanged', (e) => {
        if (e.detail.tabId === 'alert-history') {
            loadAlertHistory();
        }
    });

    // --- Channels ---
    async function loadChannels() {
        try {
            const data = await App.get('/api/alerts/channels');
            const result = data.data || data;
            const channels = Array.isArray(result) ? result : (result.items || []);
            channelsList = channels;
            renderChannels(channels);
        } catch (e) {
            App.toast('加载告警渠道失败: ' + e.message, 'error');
        }
    }

    function renderChannels(channels) {
        const tbody = document.getElementById('channels-body');
        if (channels.length === 0) {
            tbody.innerHTML = '<tr><td colspan="5" class="empty-state">暂无告警渠道</td></tr>';
            return;
        }
        tbody.innerHTML = channels.map(c => {
            const enabledBadge = c.enabled ?
                '<span class="badge badge-success">启用</span>' :
                '<span class="badge badge-gray">禁用</span>';
            return `
                <tr>
                    <td><strong>${App.escapeHtml(c.name)}</strong></td>
                    <td>${App.escapeHtml(c.type || '-')}</td>
                    <td>${App.escapeHtml(c.config_json || '-')}</td>
                    <td>${enabledBadge}</td>
                    <td>
                        <button class="btn btn-sm btn-secondary" onclick="testChannel('${c.id}')">测试</button>
                        <button class="btn btn-sm btn-secondary" onclick="editChannel('${c.id}')">编辑</button>
                        <button class="btn btn-sm btn-danger" onclick="deleteChannel('${c.id}')">删除</button>
                    </td>
                </tr>
            `;
        }).join('');
    }

    window.testChannel = async function(id) {
        try {
            await App.post(`/api/alerts/channels/${id}/test`, {});
            App.toast('测试消息已发送', 'success');
        } catch (e) {
            App.toast('测试失败: ' + e.message, 'error');
        }
    };

    window.editChannel = function(id) {
        const c = channelsList.find(ch => ch.id === id);
        if (!c) {
            App.toast('未找到渠道信息', 'error');
            return;
        }
        showAddChannelModal(c);
    };

    window.deleteChannel = async function(id) {
        if (!App.confirm('确定要删除此告警渠道吗？')) return;
        try {
            await App.delete(`/api/alerts/channels/${id}`);
            App.toast('渠道已删除', 'success');
            loadChannels();
        } catch (e) {
            App.toast('删除失败: ' + e.message, 'error');
        }
    };

    function showAddChannelModal(existing) {
        const isEdit = !!existing;
        const html = `
            <form id="channel-form">
                <div class="form-group">
                    <label>名称</label>
                    <input type="text" id="channel-name" required placeholder="告警渠道名称" value="${App.escapeHtml(existing?.name || '')}">
                </div>
                <div class="form-group">
                    <label>类型</label>
                    <select id="channel-type">
                        <option value="lark" ${existing?.type === 'lark' ? 'selected' : ''}>Lark (飞书)</option>
                        <option value="telegram" ${existing?.type === 'telegram' ? 'selected' : ''}>Telegram</option>
                    </select>
                </div>
                <div class="form-group">
                    <label>配置 (JSON)</label>
                    <textarea id="channel-config" required placeholder='{"url":"https://..."}'>${App.escapeHtml(existing?.config_json || '')}</textarea>
                </div>
                <div class="form-group">
                    <label><input type="checkbox" id="channel-enabled" ${(!existing || existing.enabled) ? 'checked' : ''}> 启用</label>
                </div>
            </form>
        `;
        const footer = `
            <button class="btn btn-secondary" onclick="App.closeModal()">取消</button>
            <button class="btn btn-primary" onclick="submitChannel('${isEdit ? existing.id : ''}')">${isEdit ? '保存' : '添加'}</button>
        `;
        App.showModal(isEdit ? '编辑告警渠道' : '添加告警渠道', html, footer);
    }

    window.submitChannel = async function(id) {
        const name = document.getElementById('channel-name').value.trim();
        const type = document.getElementById('channel-type').value;
        const config_json = document.getElementById('channel-config').value.trim();
        const enabled = document.getElementById('channel-enabled').checked;

        if (!name || !config_json) {
            App.toast('请填写所有必填字段', 'warning');
            return;
        }

        const body = { name, type, config_json, enabled };

        try {
            if (id) {
                await App.put(`/api/alerts/channels/${id}`, body);
            } else {
                await App.post('/api/alerts/channels', body);
            }
            App.toast(id ? '渠道已更新' : '渠道已添加', 'success');
            App.closeModal();
            loadChannels();
        } catch (e) {
            App.toast('操作失败: ' + e.message, 'error');
        }
    };

    // --- Alert History ---
    async function loadAlertHistory() {
        try {
            const params = new URLSearchParams();
            const levelFilter = document.getElementById('filter-level');
            const typeFilter = document.getElementById('filter-type');
            const statusFilter = document.getElementById('filter-status');

            if (levelFilter && levelFilter.value) params.set('level', levelFilter.value);
            if (typeFilter && typeFilter.value) params.set('type', typeFilter.value);
            if (statusFilter && statusFilter.value) params.set('status', statusFilter.value);

            const queryStr = params.toString();
            const url = queryStr ? `/api/alerts?${queryStr}` : '/api/alerts';
            const data = await App.get(url);
            const result = data.data || data;
            const alerts = Array.isArray(result) ? result : (result.items || []);

            renderAlerts(alerts);
        } catch (e) {
            App.toast('加载告警历史失败: ' + e.message, 'error');
        }
    }

    function renderAlerts(alerts) {
        const tbody = document.getElementById('alerts-body');
        if (alerts.length === 0) {
            tbody.innerHTML = '<tr><td colspan="6" class="empty-state">暂无告警记录</td></tr>';
            return;
        }
        tbody.innerHTML = alerts.map(a => {
            const levelBadge = getLevelBadge(a.level);
            const statusBadge = a.status === 'resolved'
                ? '<span class="badge badge-success">已解决</span>'
                : '<span class="badge badge-warning">未解决</span>';
            const sentChannels = (a.sent_channels && a.sent_channels.length > 0)
                ? App.escapeHtml(a.sent_channels.join(', '))
                : '-';
            return `
                <tr>
                    <td>${App.formatDate(a.created_at)}</td>
                    <td>${levelBadge}</td>
                    <td>${App.escapeHtml(a.type || '-')}</td>
                    <td class="text-sm">${App.escapeHtml(a.title || '-')}<br><small>${App.escapeHtml(a.content || '')}</small></td>
                    <td>${statusBadge}</td>
                    <td>${sentChannels}</td>
                </tr>
            `;
        }).join('');
    }

    function getLevelBadge(level) {
        switch (level) {
            case 'critical': return '<span class="badge badge-danger">严重</span>';
            case 'warning': return '<span class="badge badge-warning">警告</span>';
            case 'info': return '<span class="badge badge-info">信息</span>';
            default: return '<span class="badge badge-gray">' + App.escapeHtml(level || '-') + '</span>';
        }
    }
});
