// === Audit Logs Page Logic ===
document.addEventListener('DOMContentLoaded', () => {
    if (!App.requireAuth()) return;

    let currentPage = 1;
    const limit = 30;

    loadAuditLogs();

    document.getElementById('btn-filter-logs').addEventListener('click', () => {
        currentPage = 1;
        loadAuditLogs();
    });

    async function loadAuditLogs() {
        const actorType = document.getElementById('audit-actor-type').value;
        const targetType = document.getElementById('audit-target-type').value;

        const offset = (currentPage - 1) * limit;
        let url = `/api/audit-logs?limit=${limit}&offset=${offset}`;
        if (actorType) url += `&actor_type=${encodeURIComponent(actorType)}`;
        if (targetType) url += `&target_type=${encodeURIComponent(targetType)}`;

        try {
            const data = await App.get(url);
            const result = data.data || data;
            const logs = Array.isArray(result) ? result : (result.items || []);
            const total = result.total || logs.length;
            const totalPages = Math.max(1, Math.ceil(total / limit));

            renderLogs(logs);
            App.renderPagination('audit-pagination', currentPage, totalPages, (page) => {
                currentPage = page;
                loadAuditLogs();
            });
        } catch (e) {
            App.toast('加载审计日志失败: ' + e.message, 'error');
        }
    }

    function renderLogs(logs) {
        const tbody = document.getElementById('audit-body');
        if (logs.length === 0) {
            tbody.innerHTML = '<tr><td colspan="6" class="empty-state">暂无审计日志</td></tr>';
            return;
        }
        tbody.innerHTML = logs.map(log => `
            <tr>
                <td>${App.formatDate(log.created_at)}</td>
                <td>${App.escapeHtml(log.actor_type || '-')} ${App.escapeHtml(log.actor_id || '')}</td>
                <td><span class="badge badge-info">${App.escapeHtml(log.action || '-')}</span></td>
                <td>${App.escapeHtml(log.target_type || '')} ${App.escapeHtml(log.target_id || '')}</td>
                <td class="text-sm">${App.escapeHtml(log.detail || '-')}</td>
                <td class="text-sm">${App.escapeHtml(log.ip || '-')}</td>
            </tr>
        `).join('');
    }
});
