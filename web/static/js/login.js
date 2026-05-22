// login.js - Login page logic
document.addEventListener('DOMContentLoaded', function() {
    const loginForm = document.getElementById('login-form');
    const readonlyForm = document.getElementById('readonly-login-form');

    if (loginForm) {
        loginForm.addEventListener('submit', async function(e) {
            e.preventDefault();
            const username = document.getElementById('username').value.trim();
            const password = document.getElementById('password').value;

            if (!username || !password) {
                App.toast('请输入用户名和密码', 'error');
                return;
            }

            try {
                const resp = await App.post('/api/auth/login', {
                    username: username,
                    password: password
                });
                localStorage.setItem('token', resp.data.token);
                window.location.href = '/';
            } catch (err) {
                App.toast(err.message || '登录失败', 'error');
            }
        });
    }

    if (readonlyForm) {
        readonlyForm.addEventListener('submit', async function(e) {
            e.preventDefault();
            const password = document.getElementById('readonly-password').value;

            if (!password) {
                App.toast('请输入查看密码', 'error');
                return;
            }

            try {
                const resp = await App.post('/api/auth/readonly-login', {
                    password: password
                });
                localStorage.setItem('token', resp.data.token);
                window.location.href = '/';
            } catch (err) {
                App.toast(err.message || '登录失败', 'error');
            }
        });
    }
});
