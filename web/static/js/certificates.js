// certificates.js - Certificate management page logic
document.addEventListener('DOMContentLoaded', function() {
    if (!App.requireAuth()) return;
    loadCertificates();
    setupCertificateEvents();
});

async function loadCertificates() {
    try {
        const resp = await App.get('/api/certificates/');
        const certs = resp.data;
        renderCertificateList(certs);
    } catch (err) {
        App.toast('加载证书列表失败: ' + err.message, 'error');
    }
}

function renderCertificateList(certs) {
    const tbody = document.getElementById('certificates-tbody');
    if (!tbody) return;

    if (!certs || certs.length === 0) {
        tbody.innerHTML = '<tr><td colspan="8" style="text-align:center;">暂无证书</td></tr>';
        return;
    }

    tbody.innerHTML = certs.map(cert => {
        const days = App.daysUntil(cert.expire_at);
        let statusClass = '';
        let statusText = '';
        if (days !== null) {
            if (days <= 0) {
                statusClass = 'text-danger';
                statusText = '已过期';
            } else if (days <= 15) {
                statusClass = 'text-warning';
                statusText = days + '天后过期';
            } else {
                statusClass = 'text-success';
                statusText = days + '天后过期';
            }
        }

        const domains = Array.isArray(cert.domains) ? cert.domains.join(', ') : '';
        const machineCount = cert.machine_count || 0;

        return `<tr>
            <td title="${App.escapeHtml(cert.id)}">${App.escapeHtml(cert.id ? cert.id.substring(0, 8) + '...' : '')}</td>
            <td>${App.escapeHtml(cert.name)}</td>
            <td>${App.escapeHtml(domains)}</td>
            <td>${App.escapeHtml(cert.source)}</td>
            <td>${App.formatDate(cert.expire_at)}<br><span class="${statusClass}">${statusText}</span></td>
            <td>${cert.auto_renew ? '是' : '否'}</td>
            <td>${machineCount}</td>
            <td>
                <button class="btn btn-sm btn-info" onclick="viewCertificate('${cert.id}')">详情</button>
                <button class="btn btn-sm btn-danger" onclick="deleteCertificate('${cert.id}')">删除</button>
            </td>
        </tr>`;
    }).join('');
}

function setupCertificateEvents() {
    // Upload certificate form
    const uploadForm = document.getElementById('upload-cert-form');
    if (uploadForm) {
        uploadForm.addEventListener('submit', async function(e) {
            e.preventDefault();
            await uploadCertificate();
        });
    }

    // Issue via Cloudflare form
    const cloudflareForm = document.getElementById('issue-cloudflare-form');
    if (cloudflareForm) {
        cloudflareForm.addEventListener('submit', async function(e) {
            e.preventDefault();
            await issueCloudflare();
        });
    }

    // Manual DNS start form
    const manualDnsForm = document.getElementById('manual-dns-form');
    if (manualDnsForm) {
        manualDnsForm.addEventListener('submit', async function(e) {
            e.preventDefault();
            await startManualDNS();
        });
    }
}

async function uploadCertificate() {
    const name = document.getElementById('cert-name').value.trim();
    const certFileInput = document.getElementById('cert-file');
    const keyFileInput = document.getElementById('key-file');
    const chainFileInput = document.getElementById('chain-file');
    const autoRenew = document.getElementById('cert-auto-renew') ? document.getElementById('cert-auto-renew').checked : false;

    if (!name) {
        App.toast('请输入证书名称', 'error');
        return;
    }

    if (!certFileInput || !certFileInput.files[0]) {
        App.toast('请选择证书文件', 'error');
        return;
    }

    if (!keyFileInput || !keyFileInput.files[0]) {
        App.toast('请选择私钥文件', 'error');
        return;
    }

    try {
        const certPem = await readFileAsText(certFileInput.files[0]);
        const keyPem = await readFileAsText(keyFileInput.files[0]);
        let chainPem = '';
        if (chainFileInput && chainFileInput.files[0]) {
            chainPem = await readFileAsText(chainFileInput.files[0]);
        }

        // Convert to base64 bytes (the backend expects []byte which JSON encodes as base64)
        const body = {
            name: name,
            cert_pem: btoa(certPem),
            key_pem: btoa(keyPem),
            auto_renew: autoRenew
        };
        if (chainPem) {
            body.chain_pem = btoa(chainPem);
        }

        await App.post('/api/certificates/', body);
        App.toast('证书上传成功', 'success');
        App.closeModal();
        loadCertificates();
    } catch (err) {
        App.toast('上传证书失败: ' + err.message, 'error');
    }
}

async function issueCloudflare() {
    const name = document.getElementById('cf-cert-name').value.trim();
    const domainsStr = document.getElementById('cf-domains').value.trim();
    const thirdpartDnsId = document.getElementById('cf-dns-id').value.trim();
    const autoRenew = document.getElementById('cf-auto-renew') ? document.getElementById('cf-auto-renew').checked : false;

    if (!domainsStr) {
        App.toast('请输入域名', 'error');
        return;
    }
    if (!thirdpartDnsId) {
        App.toast('请选择DNS配置', 'error');
        return;
    }

    const domains = domainsStr.split(/[,\n]/).map(d => d.trim()).filter(d => d);

    try {
        await App.post('/api/certificates/issue/cloudflare', {
            name: name || domains[0],
            domains: domains,
            thirdpart_dns_id: thirdpartDnsId,
            auto_renew: autoRenew
        });
        App.toast('证书签发成功', 'success');
        App.closeModal();
        loadCertificates();
    } catch (err) {
        App.toast('签发失败: ' + err.message, 'error');
    }
}

async function startManualDNS() {
    const name = document.getElementById('mdns-cert-name').value.trim();
    const domainsStr = document.getElementById('mdns-domains').value.trim();
    const email = document.getElementById('mdns-email').value.trim();

    if (!domainsStr) {
        App.toast('请输入域名', 'error');
        return;
    }

    const domains = domainsStr.split(/[,\n]/).map(d => d.trim()).filter(d => d);

    try {
        const resp = await App.post('/api/certificates/issue/manual-dns/start', {
            name: name || domains[0],
            domains: domains,
            email: email
        });
        const data = resp.data;
        showManualDNSChallenges(data);
    } catch (err) {
        App.toast('启动手动DNS验证失败: ' + err.message, 'error');
    }
}

function showManualDNSChallenges(data) {
    let html = '<p>请添加以下DNS TXT记录，然后点击"完成验证"：</p>';
    html += '<table class="table"><thead><tr><th>域名</th><th>TXT记录名</th><th>TXT记录值</th></tr></thead><tbody>';

    if (data.challenges && Array.isArray(data.challenges)) {
        data.challenges.forEach(ch => {
            html += `<tr>
                <td>${App.escapeHtml(ch.domain || '')}</td>
                <td>${App.escapeHtml(ch.txt_record_name || '')}</td>
                <td><code>${App.escapeHtml(ch.txt_record_value || '')}</code></td>
            </tr>`;
        });
    }
    html += '</tbody></table>';
    html += `<input type="hidden" id="mdns-session-id" value="${App.escapeHtml(data.session_id)}">`;
    html += `<div style="margin-top:12px;">
        <label><input type="checkbox" id="mdns-complete-auto-renew"> 自动续期</label>
    </div>`;

    const footer = '<button class="btn btn-primary" onclick="completeManualDNS()">完成验证</button>';
    App.showModal('手动DNS验证 - 添加记录', html, footer);
}

async function completeManualDNS() {
    const sessionId = document.getElementById('mdns-session-id').value;
    const autoRenew = document.getElementById('mdns-complete-auto-renew') ? document.getElementById('mdns-complete-auto-renew').checked : false;

    if (!sessionId) {
        App.toast('会话ID缺失', 'error');
        return;
    }

    try {
        await App.post('/api/certificates/issue/manual-dns/complete', {
            session_id: sessionId,
            auto_renew: autoRenew
        });
        App.toast('证书签发成功', 'success');
        App.closeModal();
        loadCertificates();
    } catch (err) {
        App.toast('验证失败: ' + err.message, 'error');
    }
}

async function viewCertificate(id) {
    try {
        const resp = await App.get('/api/certificates/' + id);
        const cert = resp.data;

        const domains = Array.isArray(cert.domains) ? cert.domains.join(', ') : '';
        const html = `
            <table class="table">
                <tr><th>名称</th><td>${App.escapeHtml(cert.name)}</td></tr>
                <tr><th>域名</th><td>${App.escapeHtml(domains)}</td></tr>
                <tr><th>来源</th><td>${App.escapeHtml(cert.source)}</td></tr>
                <tr><th>签发者</th><td>${App.escapeHtml(cert.issuer)}</td></tr>
                <tr><th>过期时间</th><td>${App.formatDate(cert.expire_at)}</td></tr>
                <tr><th>自动续期</th><td>${cert.auto_renew ? '是' : '否'}</td></tr>
                <tr><th>指纹(SHA256)</th><td><code>${App.escapeHtml(cert.fingerprint_sha256)}</code></td></tr>
                <tr><th>证书链有效</th><td>${cert.chain_valid ? '是' : '否'}</td></tr>
                <tr><th>含私钥</th><td>${cert.has_private_key ? '是' : '否'}</td></tr>
                <tr><th>续期状态</th><td>${App.escapeHtml(cert.renew_status)}</td></tr>
                <tr><th>上次续期</th><td>${App.formatDate(cert.last_renew_at)}</td></tr>
                <tr><th>创建时间</th><td>${App.formatDate(cert.created_at)}</td></tr>
                <tr><th>更新时间</th><td>${App.formatDate(cert.updated_at)}</td></tr>
            </table>
        `;
        App.showModal('证书详情', html, '');
    } catch (err) {
        App.toast('获取证书详情失败: ' + err.message, 'error');
    }
}

async function deleteCertificate(id) {
    if (!App.confirm('确定要删除此证书吗？')) return;

    try {
        await App.delete('/api/certificates/' + id);
        App.toast('证书已删除', 'success');
        loadCertificates();
    } catch (err) {
        App.toast('删除失败: ' + err.message, 'error');
    }
}

function readFileAsText(file) {
    return new Promise((resolve, reject) => {
        const reader = new FileReader();
        reader.onload = () => resolve(reader.result);
        reader.onerror = () => reject(new Error('读取文件失败'));
        reader.readAsText(file);
    });
}
