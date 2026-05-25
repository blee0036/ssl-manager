// machines.js - Machine management page logic
document.addEventListener('DOMContentLoaded', function() {
    if (!App.requireAuth()) return;
    loadMachines();
    setupMachineEvents();
});

// --- Machine List ---
async function loadMachines(status, search) {
    try {
        let url = '/api/machines';
        const params = [];
        if (status) params.push('status=' + encodeURIComponent(status));
        if (search) params.push('search=' + encodeURIComponent(search));
        if (params.length > 0) url += '?' + params.join('&');

        const resp = await App.get(url);
        const machines = resp.data;
        renderMachineList(machines);
    } catch (err) {
        App.toast('加载机器列表失败: ' + err.message, 'error');
    }
}

function renderMachineList(machines) {
    const tbody = document.getElementById('machines-tbody');
    if (!tbody) return;

    if (!machines || machines.length === 0) {
        tbody.innerHTML = '<tr><td colspan="7" style="text-align:center;">暂无机器</td></tr>';
        return;
    }

    tbody.innerHTML = machines.map(m => {
        const tags = Array.isArray(m.tags) ? m.tags.join(', ') : '';
        const statusClass = m.status === 'online' ? 'text-success' : 'text-muted';
        return `<tr>
            <td>${App.escapeHtml(m.name)}</td>
            <td>${App.escapeHtml(m.ip)}</td>
            <td class="${statusClass}">${App.escapeHtml(m.status)}</td>
            <td>${App.escapeHtml(tags)}</td>
            <td>${App.escapeHtml(m.agent_version)}</td>
            <td>${App.formatDate(m.last_heartbeat_at)}</td>
            <td>
                <button class="btn btn-sm btn-info" onclick="viewMachine('${m.id}')">详情</button>
                <button class="btn btn-sm btn-warning" onclick="showMachineCerts('${m.id}')">部署配置</button>
                ${App._currentRole !== 'readonly' ? `<button class="btn btn-sm btn-danger" onclick="deleteMachine('${m.id}')">删除</button>` : ''}
            </td>
        </tr>`;
    }).join('');
}

function setupMachineEvents() {
    const createForm = document.getElementById('create-machine-form');
    if (createForm) {
        createForm.addEventListener('submit', async function(e) {
            e.preventDefault();
            await createMachine();
        });
    }

    const filterForm = document.getElementById('machine-filter-form');
    if (filterForm) {
        filterForm.addEventListener('submit', function(e) {
            e.preventDefault();
            const status = document.getElementById('filter-status') ? document.getElementById('filter-status').value : '';
            const search = document.getElementById('filter-search') ? document.getElementById('filter-search').value.trim() : '';
            loadMachines(status, search);
        });
    }
}

async function createMachine() {
    const name = document.getElementById('machine-name').value.trim();
    const ip = document.getElementById('machine-ip').value.trim();
    const tagsStr = document.getElementById('machine-tags') ? document.getElementById('machine-tags').value.trim() : '';
    const remark = document.getElementById('machine-remark') ? document.getElementById('machine-remark').value.trim() : '';

    if (!name) {
        App.toast('请输入机器名称', 'error');
        return;
    }

    const tags = tagsStr ? tagsStr.split(/[,，]/).map(t => t.trim()).filter(t => t) : [];

    try {
        const resp = await App.post('/api/machines', {
            name: name,
            ip: ip,
            tags: tags,
            remark: remark
        });
        const data = resp.data;
        const serverUrl = window.location.origin;
        const machineId = data.machine ? data.machine.id : (data.id || '');
        const token = data.agent_token || '';
        const installCmd = `curl -fsSL ${serverUrl}/api/agent/install.sh | bash -s -- --server-url ${serverUrl} --machine-id ${machineId} --agent-token ${token}`;
        const html = `
            <p>机器创建成功！</p>
            <p><strong>安装命令（仅显示一次）：</strong></p>
            <pre style="word-break:break-all;background:#f5f5f5;padding:12px;border-radius:4px;">${App.escapeHtml(installCmd)}</pre>
            <p>请复制上述命令到目标机器执行，完成 Agent 安装。</p>
        `;
        App.showModal('机器创建成功', html, '');
        loadMachines();
    } catch (err) {
        App.toast('创建机器失败: ' + err.message, 'error');
    }
}

async function viewMachine(id) {
    try {
        const resp = await App.get('/api/machines/' + id);
        const m = resp.data;
        const tags = Array.isArray(m.tags) ? m.tags.join(', ') : '';

        const html = `
            <table class="table">
                <tr><th>名称</th><td>${App.escapeHtml(m.name)}</td></tr>
                <tr><th>IP</th><td>${App.escapeHtml(m.ip)}</td></tr>
                <tr><th>主机名</th><td>${App.escapeHtml(m.hostname)}</td></tr>
                <tr><th>操作系统</th><td>${App.escapeHtml(m.os)}</td></tr>
                <tr><th>架构</th><td>${App.escapeHtml(m.arch)}</td></tr>
                <tr><th>标签</th><td>${App.escapeHtml(tags)}</td></tr>
                <tr><th>备注</th><td>${App.escapeHtml(m.remark)}</td></tr>
                <tr><th>状态</th><td>${App.escapeHtml(m.status)}</td></tr>
                <tr><th>Agent版本</th><td>${App.escapeHtml(m.agent_version)}</td></tr>
                <tr><th>最后心跳</th><td>${App.formatDate(m.last_heartbeat_at)}</td></tr>
                <tr><th>创建时间</th><td>${App.formatDate(m.created_at)}</td></tr>
                <tr><th>更新时间</th><td>${App.formatDate(m.updated_at)}</td></tr>
            </table>
        `;
        const footer = App._currentRole !== 'readonly' ? `
            <button class="btn btn-warning" onclick="regenerateToken('${m.id}')">重新生成Token</button>
            <button class="btn btn-secondary" onclick="revokeToken('${m.id}')">吊销Token</button>
            <button class="btn btn-info" onclick="getInstallCommand('${m.id}')">安装命令</button>
        ` : '';
        App.showModal('机器详情 - ' + m.name, html, footer);
    } catch (err) {
        App.toast('获取机器详情失败: ' + err.message, 'error');
    }
}

async function deleteMachine(id) {
    if (!App.confirm('确定要删除此机器吗？')) return;

    try {
        await App.delete('/api/machines/' + id);
        App.toast('机器已删除', 'success');
        loadMachines();
    } catch (err) {
        App.toast('删除失败: ' + err.message, 'error');
    }
}

async function regenerateToken(id) {
    if (!App.confirm('重新生成Token将使旧Token失效，确定继续？')) return;

    try {
        const resp = await App.post('/api/machines/' + id + '/regenerate-token');
        const data = resp.data;
        const serverUrl = window.location.origin;
        const token = data.agent_token || '';
        const installCmd = `curl -fsSL ${serverUrl}/api/agent/install.sh | bash -s -- --server-url ${serverUrl} --machine-id ${id} --agent-token ${token}`;
        const html = `
            <p><strong>新的安装命令（仅显示一次）：</strong></p>
            <pre style="word-break:break-all;background:#f5f5f5;padding:12px;border-radius:4px;">${App.escapeHtml(installCmd)}</pre>
            <p>请复制上述命令到目标机器执行，完成 Agent 重新安装。</p>
        `;
        App.showModal('Token已重新生成', html, '');
    } catch (err) {
        App.toast('重新生成Token失败: ' + err.message, 'error');
    }
}

async function revokeToken(id) {
    if (!App.confirm('吊销Token后Agent将无法连接，确定继续？')) return;

    try {
        await App.post('/api/machines/' + id + '/revoke-token');
        App.toast('Token已吊销', 'success');
    } catch (err) {
        App.toast('吊销Token失败: ' + err.message, 'error');
    }
}

async function getInstallCommand(id) {
    // The GET /install-command endpoint returns a placeholder token.
    // To provide a usable command, we regenerate the token (with user confirmation).
    if (!App.confirm('获取安装命令需要生成新的 Agent Token（旧 Token 将失效），确定继续？')) return;

    try {
        const resp = await App.post('/api/machines/' + id + '/regenerate-token');
        const data = resp.data;
        const serverUrl = window.location.origin;
        const token = data.agent_token || '';
        const installCmd = `curl -fsSL ${serverUrl}/api/agent/install.sh | bash -s -- --server-url ${serverUrl} --machine-id ${id} --agent-token ${token}`;
        const html = `
            <p><strong>安装命令（Token 已重新生成，仅显示一次）：</strong></p>
            <pre style="word-break:break-all;background:#f5f5f5;padding:12px;border-radius:4px;">${App.escapeHtml(installCmd)}</pre>
            <p>请复制上述命令到目标机器执行，完成 Agent 安装。</p>
        `;
        App.showModal('安装命令', html, '');
    } catch (err) {
        App.toast('获取安装命令失败: ' + err.message, 'error');
    }
}

// --- Machine Certificates (Deploy Configs) ---
async function showMachineCerts(machineId) {
    try {
        const resp = await App.get('/api/machines/' + machineId + '/certificates');
        const configs = resp.data;
        renderMachineCerts(machineId, configs);
    } catch (err) {
        App.toast('加载部署配置失败: ' + err.message, 'error');
    }
}

function renderMachineCerts(machineId, configs) {
    let html = '<table class="table"><thead><tr><th>证书ID</th><th>证书路径</th><th>私钥路径</th><th>部署状态</th><th>操作</th></tr></thead><tbody>';

    if (!configs || configs.length === 0) {
        html += '<tr><td colspan="5" style="text-align:center;">暂无部署配置</td></tr>';
    } else {
        configs.forEach(mc => {
            html += `<tr>
                <td>${App.escapeHtml(mc.certificate_id)}</td>
                <td>${App.escapeHtml(mc.cert_path)}</td>
                <td>${App.escapeHtml(mc.private_key_path)}</td>
                <td>${App.escapeHtml(mc.last_deploy_status)}</td>
                <td>
                    ${App._currentRole !== 'readonly' ? `<button class="btn btn-sm btn-primary" onclick="triggerDeploy('${machineId}','${mc.id}')">部署</button>` : ''}
                    <button class="btn btn-sm btn-info" onclick="viewDeployLogs('${machineId}','${mc.id}')">日志</button>
                    ${App._currentRole !== 'readonly' ? `<button class="btn btn-sm btn-danger" onclick="deleteMachineCert('${machineId}','${mc.id}')">删除</button>` : ''}
                </td>
            </tr>`;
        });
    }
    html += '</tbody></table>';
    if (App._currentRole !== 'readonly') {
        html += `<button class="btn btn-success" onclick="showAddMachineCertForm('${machineId}')">添加部署配置</button>`;
    }

    App.showModal('部署配置', html, '');
}

async function viewDeployLogs(machineId, mcId) {
    try {
        const resp = await App.get('/api/machines/' + machineId + '/certificates/' + mcId + '/deployment-logs');
        const logs = resp.data || [];

        let html = '';
        if (!logs || logs.length === 0) {
            html = '<p style="text-align:center;">暂无部署日志</p>';
        } else {
            html = '<table class="table"><thead><tr><th>时间</th><th>状态</th><th>详情</th></tr></thead><tbody>';
            logs.forEach(log => {
                const statusClass = log.status === 'success' ? 'text-success' : 'text-danger';

                // Build detail from command_outputs and error_message
                let detail = '';
                if (log.error_message) {
                    detail += '<div class="text-danger"><strong>错误：</strong>' + App.escapeHtml(log.error_message) + '</div>';
                }
                if (log.command_outputs && Array.isArray(log.command_outputs) && log.command_outputs.length > 0) {
                    log.command_outputs.forEach(cmd => {
                        const cmdStatus = cmd.exit_code === 0 ? '✓' : '✗';
                        const timedOut = cmd.timed_out ? ' [超时]' : '';
                        detail += `<div style="margin-top:4px;"><code>${cmdStatus} ${App.escapeHtml(cmd.command)}${timedOut}</code> (exit: ${cmd.exit_code})`;
                        if (cmd.stdout) {
                            detail += `<pre style="max-height:60px;overflow:auto;font-size:11px;margin:2px 0;background:#f9f9f9;padding:4px;">${App.escapeHtml(cmd.stdout)}</pre>`;
                        }
                        if (cmd.stderr) {
                            detail += `<pre style="max-height:60px;overflow:auto;font-size:11px;margin:2px 0;background:#fff3f3;padding:4px;">${App.escapeHtml(cmd.stderr)}</pre>`;
                        }
                        detail += '</div>';
                    });
                }
                if (!detail) {
                    detail = '-';
                }

                html += `<tr>
                    <td>${App.formatDate(log.created_at)}</td>
                    <td class="${statusClass}">${App.escapeHtml(log.status)}</td>
                    <td style="max-width:400px;overflow:auto;">${detail}</td>
                </tr>`;
            });
            html += '</tbody></table>';
        }

        App.showModal('部署日志', html, '');
    } catch (err) {
        App.toast('加载部署日志失败: ' + err.message, 'error');
    }
}

async function showAddMachineCertForm(machineId) {
    // Load certificate list for dropdown
    let certOptions = '<option value="">请选择证书</option>';
    try {
        const resp = await App.get('/api/certificates/');
        const certs = resp.data || [];
        certs.forEach(cert => {
            const domains = Array.isArray(cert.domains) ? cert.domains.join(', ') : '';
            const expireInfo = cert.expire_at ? ' | 过期: ' + App.formatDate(cert.expire_at) : '';
            const autoRenewInfo = cert.auto_renew ? ' | 自动续期' : ' | 手动续期';
            const shortId = cert.id ? cert.id.substring(0, 8) : '';
            certOptions += `<option value="${App.escapeHtml(cert.id)}">${App.escapeHtml(shortId)} | ${App.escapeHtml(cert.name)} | ${App.escapeHtml(domains)}${expireInfo}${autoRenewInfo}</option>`;
        });
    } catch (err) {
        // Fallback to manual input if loading fails
        certOptions = '';
    }

    let certField;
    if (certOptions) {
        certField = `<select id="mc-cert-id" class="form-control" required>${certOptions}</select>`;
    } else {
        certField = `<input type="text" id="mc-cert-id" class="form-control" required placeholder="证书ID">`;
    }

    const html = `
        <form id="add-machine-cert-form">
            <div class="form-group">
                <label>证书</label>
                ${certField}
            </div>
            <div class="form-group">
                <label>证书部署路径</label>
                <input type="text" id="mc-cert-path" class="form-control" placeholder="/etc/ssl/cert.pem" required>
            </div>
            <div class="form-group">
                <label>私钥部署路径</label>
                <input type="text" id="mc-key-path" class="form-control" placeholder="/etc/ssl/key.pem" required>
            </div>
            <div class="form-group">
                <label>部署后执行命令</label>
                <input type="text" id="mc-post-commands" class="form-control" placeholder="systemctl reload nginx">
            </div>
            <button type="submit" class="btn btn-primary">保存</button>
        </form>
    `;
    App.showModal('添加部署配置', html, '');

    setTimeout(() => {
        const form = document.getElementById('add-machine-cert-form');
        if (form) {
            form.addEventListener('submit', async function(e) {
                e.preventDefault();
                await createMachineCert(machineId);
            });
        }
    }, 100);
}

async function createMachineCert(machineId) {
    const certificateId = document.getElementById('mc-cert-id').value.trim();
    const certPath = document.getElementById('mc-cert-path').value.trim();
    const privateKeyPath = document.getElementById('mc-key-path').value.trim();
    const postDeployCommands = document.getElementById('mc-post-commands').value.trim();

    if (!certificateId || !certPath || !privateKeyPath) {
        App.toast('请填写必填字段', 'error');
        return;
    }

    try {
        await App.post('/api/machines/' + machineId + '/certificates', {
            certificate_id: certificateId,
            cert_path: certPath,
            private_key_path: privateKeyPath,
            post_deploy_commands: postDeployCommands
        });
        App.toast('部署配置已创建', 'success');
        showMachineCerts(machineId);
    } catch (err) {
        App.toast('创建部署配置失败: ' + err.message, 'error');
    }
}

async function triggerDeploy(machineId, mcId) {
    try {
        await App.post('/api/machines/' + machineId + '/certificates/' + mcId + '/deploy');
        App.toast('部署已触发', 'success');
    } catch (err) {
        App.toast('触发部署失败: ' + err.message, 'error');
    }
}

async function deleteMachineCert(machineId, mcId) {
    if (!App.confirm('确定要删除此部署配置吗？')) return;

    try {
        await App.delete('/api/machines/' + machineId + '/certificates/' + mcId);
        App.toast('部署配置已删除', 'success');
        showMachineCerts(machineId);
    } catch (err) {
        App.toast('删除部署配置失败: ' + err.message, 'error');
    }
}
