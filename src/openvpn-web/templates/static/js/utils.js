!(function () {
  'use strict';

  // Message 类 - 用于显示消息提示
  class Message {
    constructor() {
      this.div = document.createElement('div');
      this.div.id = 'message';
      this.div.className = 'message';

      if (!document.getElementById('message')) {
        document.body.appendChild(this.div);
      }
    }

    open(type = 'info', content, duration, onClose) {
      duration = duration == 0 ? duration : (duration || 3) * 1000;

      const html = `
    <div class="message-notice">
      <div class="message-content message-${type}">
        <span class="message-anticon">
          ${this.icon}
        </span>
        <span class="message-text"></span>
      </div>
    </div>
    `;

      const $msgDiv = $(html).appendTo('#message');
      $msgDiv.find('.message-text').text(content);
      $msgDiv.removeClass('move-up-leave').addClass('move-up-enter');
      setTimeout(() => {
        $msgDiv.removeClass('move-up-enter');
      }, 300);
      if (duration != 0) {
        setTimeout(function () {
          $msgDiv.removeClass('move-up-enter').addClass('move-up-leave');
          setTimeout(() => {
            $msgDiv.removeClass('move-up-leave').remove();
          }, 300);
          if (onClose && typeof onClose === 'function') {
            onClose();
          }
        }, duration);
      }
    }

    info(content, duration, onClose) {
      this.icon = `
    <svg xmlns="http://www.w3.org/2000/svg" width="1em" height="1em" fill="currentColor" viewBox="0 0 16 16">
      <path d="M16 8A8 8 0 1 1 0 8a8 8 0 0 1 16 0zM8 4a.905.905 0 0 0-.9.995l.35 3.507a.552.552 0 0 0 1.1 0l.35-3.507A.905.905 0 0 0 8 4zm.002 6a1 1 0 1 0 0 2 1 1 0 0 0 0-2z"/>
    </svg>`;

      this.open('info', content, duration, onClose);
    }

    success(content, duration, onClose) {
      this.icon = `
    <svg xmlns="http://www.w3.org/2000/svg" width="1em" height="1em" fill="currentColor" viewBox="0 0 16 16">
      <path d="M16 8A8 8 0 1 1 0 8a8 8 0 0 1 16 0zm-3.97-3.03a.75.75 0 0 0-1.08.022L7.477 9.417 5.384 7.323a.75.75 0 0 0-1.06 1.06L6.97 11.03a.75.75 0 0 0 1.079-.02l3.992-4.99a.75.75 0 0 0-.01-1.05z"/>
    </svg>`;

      this.open('success', content, duration, onClose);
    }

    warning(content, duration, onClose) {
      this.icon = `
    <svg xmlns="http://www.w3.org/2000/svg" width="1em" height="1em" fill="currentColor" viewBox="0 0 16 16">
      <path d="M16 8A8 8 0 1 1 0 8a8 8 0 0 1 16 0zM8 4a.905.905 0 0 0-.9.995l.35 3.507a.552.552 0 0 0 1.1 0l.35-3.507A.905.905 0 0 0 8 4zm.002 6a1 1 0 1 0 0 2 1 1 0 0 0 0-2z"/>
    </svg>`;

      this.open('warning', content, duration, onClose);
    }

    error(content, duration, onClose) {
      this.icon = `
    <svg xmlns="http://www.w3.org/2000/svg" width="1em" height="1em" fill="currentColor" viewBox="0 0 16 16">
      <path d="M16 8A8 8 0 1 1 0 8a8 8 0 0 1 16 0zM5.354 4.646a.5.5 0 1 0-.708.708L7.293 8l-2.647 2.646a.5.5 0 0 0 .708.708L8 8.707l2.646 2.647a.5.5 0 0 0 .708-.708L8.707 8l2.647-2.646a.5.5 0 0 0-.708-.708L8 7.293 5.354 4.646z"/>
    </svg>
    `;

      this.open('error', content, duration, onClose);
    }
  }

  // Only for HTML text and quoted attribute contexts, never JS/CSS/URLs.
  window.escapeHTML = (value) => String(value ?? '').replace(/[&<>"']/g, (char) => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
  })[char]);
  window.safeDownloadURL = (value) => {
    try {
      const url = new URL(value, location.origin);
      return url.origin === location.origin && /^https?:$/.test(url.protocol) ? url.href : '#';
    } catch (_) { return '#'; }
  };

  // Request 类 - 用于处理HTTP请求
  class Request {
    async csrfToken(refresh = false) {
      if (refresh) this.csrfPromise = null;
      if (!this.csrfPromise) {
        this.csrfPromise = fetch('/csrf', { credentials: 'same-origin', cache: 'no-store' })
          .then(async (response) => {
            const data = await response.json();
            if (!response.ok || typeof data.csrfToken !== 'string' || !data.csrfToken) {
              throw new Error('无法获取安全令牌，请刷新页面后重试');
            }
            return data.csrfToken;
          }).catch((error) => { this.csrfPromise = null; throw error; });
      }
      return this.csrfPromise;
    }

    async request(method, url, data = null) {
      const target = new URL(url, location.origin);
      if (target.origin !== location.origin) throw new Error('拒绝跨站请求');
      const options = { method, headers: {}, credentials: 'same-origin', redirect: 'manual' };
      try {
        if (!['GET', 'HEAD', 'OPTIONS'].includes(method)) {
          options.headers['X-CSRF-Token'] = await this.csrfToken();
          if (data instanceof FormData) {
            options.body = data;
          } else if (data) {
            options.headers['Content-Type'] = 'application/x-www-form-urlencoded; charset=UTF-8';
            options.body = new URLSearchParams(data);
          }
        }
        const response = await fetch(target.href, options);
        if (response.type === 'opaqueredirect') {
          location.assign('/login');
          throw new Error('会话已过期，请重新登录');
        }
        const body = await response.json();
        if (!response.ok) {
          const error = new Error(body?.message || response.statusText);
          error.body = body;
          throw error;
        }
        if (target.pathname === '/login' || target.pathname === '/logout') await this.csrfToken(true);
        return body;
      } catch (error) {
        // Never automatically replay mutations after a rejected token.
        if (error.message.includes('UNIQUE constraint failed: user.ip_addr')) message.error('IP已经使用');
        else if (error.message.includes('UNIQUE constraint failed: user.username')) message.error('用户名已存在');
        else message.error(error.message);
        throw error;
      }
    }

    get(url) {
      return this.request('GET', url);
    }

    put(url, data) {
      return this.request('PUT', url, data);
    }

    post(url, data) {
      return this.request('POST', url, data);
    }

    delete(url, data) {
      return this.request('DELETE', url, data);
    }

    patch(url, data) {
      return this.request('PATCH', url, data);
    }
  }

  const message = new Message();
  const request = new Request();

  window.message = message;
  window.request = request;
  $(document).on('click', 'a[href="/logout"]', function (event) {
    event.preventDefault();
    request.post('/logout').then(() => location.assign('/login')).catch(() => {});
  });
})();
