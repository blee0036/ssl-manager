// === System Config Page Logic ===
document.addEventListener('DOMContentLoaded', () => {
    if (!App.requireAuth()) return;

    let originalViewPassword = '';

    loadConfig();

    document.getElementById('system-config-form').addEventListener('submit', saveConfig);
    document.getElementById('btn-reload-config').addEventListener('click', loadConfig);

    async function loadConfig() {
        try {
            const data = await App.get('/api/system/config');
            const cfg = data.data || data;

            // Read from nested config structure
            setVal('cfg-listen-addr', cfg.server.listen_addr || '');
            setVal('cfg-external-url', cfg.server.external_url || '');
            setVal('cfg-certbot-email', cfg.certbot.email || '');
            setVal('cfg-certbot-binary', cfg.certbot.binary_path || '');
            setVal('cfg-certbot-datadir', cfg.certbot.data_dir || '');
            setVal('cfg-alert-days', cfg.alert.default_before_days || '');
            setVal('cfg-heartbeat-timeout', cfg.agent.heartbeat_timeout_seconds || '');
            setVal('cfg-poll-interval', cfg.agent.poll_interval_seconds || '');
            setVal('cfg-readonly-enabled', cfg.readonly.enabled ? 'true' : 'false');
            setVal('cfg-readonly-pwd', cfg.readonly.view_password || '');
            setVal('cfg-domain-port', cfg.domain_monitor.default_port || '');
            setVal('cfg-domain-interval', cfg.domain_monitor.interval_minutes || '');

            // Remember the original view_password for masking detection
            originalViewPassword = cfg.readonly.view_password || '';
        } catch (e) {
            App.toast('加载配置失败: ' + e.message, 'error');
        }
    }

    async function saveConfig(e) {
        e.preventDefault();

        const listenAddr = getVal('cfg-listen-addr');
        const externalUrl = getVal('cfg-external-url');
        const certbotEmail = getVal('cfg-certbot-email');
        const certbotBinary = getVal('cfg-certbot-binary');
        const certbotDataDir = getVal('cfg-certbot-datadir');
        const alertDays = getVal('cfg-alert-days');
        const heartbeatTimeout = getVal('cfg-heartbeat-timeout');
        const pollInterval = getVal('cfg-poll-interval');
        const readonlyEnabled = getVal('cfg-readonly-enabled');
        const readonlyPwd = getVal('cfg-readonly-pwd');
        const domainPort = getVal('cfg-domain-port');
        const domainInterval = getVal('cfg-domain-interval');

        // Handle sensitive field: if password is still masked, keep original value
        let viewPassword = readonlyPwd;
        if (viewPassword === '***') {
            viewPassword = originalViewPassword;
        }

        // Build nested config structure matching backend config.Config
        const body = {
            server: {
                "external_url": externalUrl || 'http://localhost:8080',
                "listen_addr": listenAddr || ':8080'
            },
            agent: {
                "heartbeat_timeout_seconds": parseInt(heartbeatTimeout) || 120,
                "poll_interval_seconds": parseInt(pollInterval) || 60
            },
            alert: {
                "default_before_days": parseInt(alertDays) || 15
            },
            certbot: {
                "binary_path": certbotBinary || 'certbot',
                "data_dir": certbotDataDir || '',
                "email": certbotEmail || ''
            },
            readonly: {
                "enabled": readonlyEnabled === 'true',
                "view_password": viewPassword || ''
            },
            domain_monitor: {
                "default_port": parseInt(domainPort) || 443,
                "interval_minutes": parseInt(domainInterval) || 60
            }
        };

        try {
            await App.put('/api/system/config', body);
            App.toast('配置已保存', 'success');
            loadConfig();
        } catch (e) {
            App.toast('保存配置失败: ' + e.message, 'error');
        }
    }

    function setVal(id, val) {
        const el = document.getElementById(id);
        if (el) el.value = val;
    }

    function getVal(id) {
        const el = document.getElementById(id);
        return el ? el.value.trim() : '';
    }
});
