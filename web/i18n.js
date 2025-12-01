// i18n.js
const messages = {
    "Usque Login": {
        "zh": "Usque 登录"
    },
    "Username:": {
        "zh": "用户名:"
    },
    "Password:": {
        "zh": "密码:"
    },
    "Login": {
        "zh": "登录"
    },
    "Usque Configuration": {
        "zh": "Usque 配置"
    },
    "Web UI": {
        "zh": "Web UI"
    },
    "New Password (leave blank to keep current):": {
        "zh": "新密码 (留空则不修改):"
    },
    "Warp Details": {
        "zh": "Warp 详情"
    },
    "Private Key:": {
        "zh": "私钥:"
    },
    "Endpoint IPv4:": {
        "zh": "端点 IPv4:"
    },
    "Endpoint IPv6:": {
        "zh": "端点 IPv6:"
    },
    "Endpoint Public Key:": {
        "zh": "端点公钥:"
    },
    "License:": {
        "zh": "许可证:"
    },
    "Device ID:": {
        "zh": "设备 ID:"
    },
    "Access Token:": {
        "zh": "访问令牌:"
    },
    "Assigned IPv4:": {
        "zh": "分配的 IPv4:"
    },
    "Assigned IPv6:": {
        "zh": "分配的 IPv6:"
    },
    "Save": {
        "zh": "保存"
    },
    "Usque Registration": {
        "zh": "Usque 注册"
    },
    "No configuration file found. Please register a new device.": {
        "zh": "未找到配置文件。请注册一个新设备。"
    },
    "Device Name (optional):": {
        "zh": "设备名 (可选):"
    },
    "Team JWT (for Zero Trust, optional):": {
        "zh": "团队 JWT (用于 Zero Trust, 可选):"
    },
    "Register": {
        "zh": "注册"
    }
};

function getLang() {
    let lang = navigator.language || navigator.userLanguage;
    lang = lang.substr(0, 2);
    return lang;
}

function translatePage() {
    const lang = getLang();
    if (lang !== "zh") {
        return;
    }

    document.querySelectorAll('[data-i18n]').forEach(elem => {
        const key = elem.getAttribute('data-i18n');
        if (messages[key] && messages[key][lang]) {
            if (elem.tagName === 'INPUT' || elem.tagName === 'TEXTAREA') {
                elem.placeholder = messages[key][lang];
            } else {
                elem.innerHTML = messages[key][lang];
            }
        }
    });
}

window.addEventListener('DOMContentLoaded', (event) => {
    translatePage();
});
