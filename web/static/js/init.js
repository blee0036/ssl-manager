// init.js - System initialization page logic
document.addEventListener('DOMContentLoaded', async function() {
    await checkInitStatus();
});

async function checkInitStatus() {
    try {
        const resp = await App.get('/init/status');
        const data = resp.data;
        showPhase(data.phase);
    } catch (err) {
        if (err.code === 403) {
            // System already initialized, redirect to login
            window.location.href = '/login';
            return;
        }
        App.toast('无法获取初始化状态: ' + err.message, 'error');
    }
}

function showPhase(phase) {
    const adminSection = document.getElementById('init-admin-section');
    const configSection = document.getElementById('init-config-section');

    if (phase === 'needs_admin') {
        if (adminSection) adminSection.style.display = 'block';
        if (configSection) configSection.style.display = 'none';
        setupAdminForm();
    } else if (phase === 'needs_config') {
        if (adminSection) adminSection.style.display = 'none';
        if (configSection) configSection.style.display = 'block';
        setupConfigForm();
    }
}

function setupAdminForm() {
    const form = document.getElementById('init-admin-form');
    if (!form) return;

    form.addEventListener('submit', async function(e) {
        e.preventDefault();
        const username = document.getElementById('admin-username').value.trim();
        const password = document.getElementById('admin-password').value;

        if (!username || !password) {
            App.toast('请输入用户名和密码', 'error');
            return;
        }

        try {
            await App.post('/init/admin', {
                username: username,
                password: password
            });
            App.toast('管理员创建成功', 'success');
            showPhase('needs_config');
        } catch (err) {
            App.toast(err.message || '创建管理员失败', 'error');
        }
    });
}

function setupConfigForm() {
    const form = document.getElementById('init-config-form');
    if (!form) return;

    form.addEventListener('submit', async function(e) {
        e.preventDefault();

        const config = {
            server: {
                external_url: document.getElementById('cfg-external-url').value.trim() || 'http://localhost:8080',
                listen_addr: document.getElementById('cfg-listen-addr').value.trim() || ':8080'
            },
            agent: {
                heartbeat_timeout_seconds: parseInt(document.getElementById('cfg-heartbeat-timeout').value) || 120,
                poll_interval_seconds: parseInt(document.getElementById('cfg-poll-interval').value) || 60
            },
            alert: {
                default_before_days: parseInt(document.getElementById('cfg-alert-days').value) || 15
            },
            certbot: {
                binary_path: document.getElementById('cfg-certbot-path').value.trim() || 'certbot',
                data_dir: document.getElementById('cfg-certbot-datadir').value.trim() || '',
                email: document.getElementById('cfg-certbot-email').value.trim() || ''
            },
            readonly: {
                enabled: document.getElementById('cfg-readonly-enabled') ? document.getElementById('cfg-readonly-enabled').checked : false,
                view_password: document.getElementById('cfg-readonly-password') ? document.getElementById('cfg-readonly-password').value : ''
            },
            domain_monitor: {
                default_port: parseInt(document.getElementById('cfg-monitor-port').value) || 443,
                interval_minutes: parseInt(document.getElementById('cfg-monitor-interval').value) || 60
            }
        };

        try {
            await App.post('/init/config', config);
            App.toast('配置保存成功，即将进入系统', 'success');
            setTimeout(() => { window.location.href = '/login'; }, 1500);
        } catch (err) {
            App.toast(err.message || '保存配置失败', 'error');
        }
    });
}
