// === Users Page Logic ===
document.addEventListener('DOMContentLoaded', () => {
    if (!App.requireAuth()) return;

    let usersCache = [];

    loadUsers();

    document.getElementById('btn-add-user').addEventListener('click', () => showAddModal());

    async function loadUsers() {
        try {
            const data = await App.get('/api/users');
            const result = data.data || data;
            const users = Array.isArray(result) ? result : (result.items || []);
            usersCache = users;
            renderUsers(users);
        } catch (e) {
            App.toast('加载用户列表失败: ' + e.message, 'error');
        }
    }

    function renderUsers(users) {
        const tbody = document.getElementById('users-body');
        if (users.length === 0) {
            tbody.innerHTML = '<tr><td colspan="5" class="empty-state">暂无用户</td></tr>';
            return;
        }
        tbody.innerHTML = users.map(u => {
            const roleBadge = u.role === 'admin' ?
                '<span class="badge badge-info">管理员</span>' :
                '<span class="badge badge-gray">普通用户</span>';
            const statusBadge = u.enabled ?
                '<span class="badge badge-success">启用</span>' :
                '<span class="badge badge-danger">禁用</span>';
            return `
                <tr>
                    <td><strong>${App.escapeHtml(u.username)}</strong></td>
                    <td>${roleBadge}</td>
                    <td>${statusBadge}</td>
                    <td>${App.formatDate(u.created_at)}</td>
                    <td>
                        <button class="btn btn-sm btn-secondary" onclick="editUser('${u.id}')">编辑角色</button>
                        <button class="btn btn-sm btn-secondary" onclick="resetPassword('${u.id}')">重置密码</button>
                        ${u.enabled ? '<button class="btn btn-sm btn-danger" onclick="disableUser(\'' + u.id + '\')">禁用</button>' : ''}
                    </td>
                </tr>
            `;
        }).join('');
    }

    window.editUser = function(id) {
        const u = usersCache.find(user => user.id === id);
        if (!u) {
            App.toast('未找到用户信息', 'error');
            return;
        }
        showEditRoleModal(u);
    };

    function showEditRoleModal(u) {
        const html = `
            <form id="role-form">
                <div class="form-group">
                    <label>用户名</label>
                    <input type="text" value="${App.escapeHtml(u.username)}" readonly>
                </div>
                <div class="form-group">
                    <label>角色</label>
                    <select id="user-role">
                        <option value="admin" ${u.role === 'admin' ? 'selected' : ''}>管理员</option>
                        <option value="user" ${u.role === 'user' ? 'selected' : ''}>普通用户</option>
                    </select>
                </div>
            </form>
        `;
        const footer = `
            <button class="btn btn-secondary" onclick="App.closeModal()">取消</button>
            <button class="btn btn-primary" onclick="submitUpdateRole('${u.id}')">保存</button>
        `;
        App.showModal('编辑角色', html, footer);
    }

    window.submitUpdateRole = async function(id) {
        const role = document.getElementById('user-role').value;
        try {
            await App.put(`/api/users/${id}`, { role });
            App.toast('角色已更新', 'success');
            App.closeModal();
            loadUsers();
        } catch (e) {
            App.toast('操作失败: ' + e.message, 'error');
        }
    };

    window.resetPassword = async function(id) {
        const html = `
            <form id="reset-pwd-form">
                <div class="form-group">
                    <label>新密码</label>
                    <input type="password" id="new-password" required minlength="6" placeholder="至少6位">
                </div>
                <div class="form-group">
                    <label>确认密码</label>
                    <input type="password" id="confirm-password" required minlength="6">
                </div>
            </form>
        `;
        const footer = `
            <button class="btn btn-secondary" onclick="App.closeModal()">取消</button>
            <button class="btn btn-primary" onclick="submitResetPassword('${id}')">重置</button>
        `;
        App.showModal('重置密码', html, footer);
    };

    window.submitResetPassword = async function(id) {
        const password = document.getElementById('new-password').value;
        const confirm = document.getElementById('confirm-password').value;

        if (password !== confirm) {
            App.toast('两次输入的密码不一致', 'warning');
            return;
        }
        if (password.length < 6) {
            App.toast('密码至少6位', 'warning');
            return;
        }

        try {
            await App.post(`/api/users/${id}/reset-password`, { new_password: password });
            App.toast('密码已重置', 'success');
            App.closeModal();
        } catch (e) {
            App.toast('重置失败: ' + e.message, 'error');
        }
    };

    window.disableUser = async function(id) {
        try {
            await App.post(`/api/users/${id}/disable`);
            App.toast('用户已禁用', 'success');
            loadUsers();
        } catch (e) {
            App.toast('操作失败: ' + e.message, 'error');
        }
    };

    function showAddModal() {
        const html = `
            <form id="user-form">
                <div class="form-group">
                    <label>用户名</label>
                    <input type="text" id="user-username" required minlength="3" placeholder="用户名">
                </div>
                <div class="form-group">
                    <label>密码</label>
                    <input type="password" id="user-password" required minlength="6" placeholder="至少6位">
                </div>
            </form>
        `;
        const footer = `
            <button class="btn btn-secondary" onclick="App.closeModal()">取消</button>
            <button class="btn btn-primary" onclick="submitCreateUser()">创建</button>
        `;
        App.showModal('添加用户', html, footer);
    }

    window.submitCreateUser = async function() {
        const username = document.getElementById('user-username').value.trim();
        const password = document.getElementById('user-password').value;

        if (!username) {
            App.toast('请输入用户名', 'warning');
            return;
        }
        if (!password || password.length < 6) {
            App.toast('密码至少6位', 'warning');
            return;
        }

        try {
            await App.post('/api/users', { username, password });
            App.toast('用户已创建', 'success');
            App.closeModal();
            loadUsers();
        } catch (e) {
            App.toast('操作失败: ' + e.message, 'error');
        }
    };
});
