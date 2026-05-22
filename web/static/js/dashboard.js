// dashboard.js - Dashboard page logic
document.addEventListener('DOMContentLoaded', function() {
    if (!App.requireAuth()) return;
    loadDashboard();
});

async function loadDashboard() {
    try {
        const resp = await App.get('/api/dashboard');
        const stats = resp.data;
        renderDashboard(stats);
    } catch (err) {
        App.toast('加载仪表盘失败: ' + err.message, 'error');
    }
}

function renderDashboard(stats) {
    setStatValue('stat-certs-total', stats.certificates_total);
    setStatValue('stat-certs-expiring', stats.certificates_expiring_15d);
    setStatValue('stat-certs-expired', stats.certificates_expired);
    setStatValue('stat-machines-total', (stats.machines_online || 0) + (stats.machines_offline || 0));
    setStatValue('stat-machines-online', stats.machines_online);
    setStatValue('stat-machines-offline', stats.machines_offline);
    setStatValue('stat-deploy-failures', stats.deploy_failures_24h);
    setStatValue('stat-renew-failures', stats.renew_failures_24h);
    setStatValue('stat-domain-anomalies', stats.domain_anomalies);

    const anomalyBadge = document.getElementById('anomaly-badge');
    if (anomalyBadge) {
        anomalyBadge.style.display = stats.has_anomalies ? 'inline-block' : 'none';
    }
}

function setStatValue(id, value) {
    const el = document.getElementById(id);
    if (el) {
        el.textContent = value !== undefined && value !== null ? value : '0';
    }
}
