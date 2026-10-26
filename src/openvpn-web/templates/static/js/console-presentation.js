// Presentation only: no endpoint, authentication or mutation policy lives here.
export function initializeForms() {
  document.querySelectorAll('.modal label:not([for])').forEach((label, index) => {
    const group = label.closest('.form-group, .row, .mb-3');
    const field = label.querySelector('input, select, textarea') || group?.querySelector('input:not([type=hidden]), select, textarea');
    if (!field) return;
    if (!field.id) field.id = 'console-field-' + index;
    label.htmlFor = field.id;
    if (field.required) {
      const required = document.createElement('small');
      required.className = 'field-required';
      required.textContent = ' 必填';
      label.append(required);
    }
  });
  document.addEventListener('invalid', event => {
    if (event.target.closest('.modal')) event.target.setAttribute('aria-invalid', 'true');
  }, true);
  document.addEventListener('input', event => {
    if (event.target.matches('.modal input, .modal textarea') && event.target.validity.valid) event.target.removeAttribute('aria-invalid');
  });
  let origin;
  for (const type of ['click', 'submit', 'change', 'focusout']) document.addEventListener(type, event => { origin = event.target; }, true);
  const pending = new WeakMap();
  // Observe existing promises only: no retry, payload or hardened Request changes.
  window.consoleRequest = new Proxy(window.request, {
    get(target, method) {
      if (!['post', 'put', 'patch', 'delete'].includes(method)) {
        const value = target[method];
        return typeof value === 'function' ? value.bind(target) : value;
      }
      return (...args) => {
        const scope = origin?.closest('.modal') || document.getElementById('consoleMain');
        let feedback = scope.querySelector('.operation-feedback');
        if (!feedback) {
          feedback = document.createElement('div');
          feedback.className = 'operation-feedback';
          feedback.setAttribute('role', 'status');
          feedback.setAttribute('aria-live', 'polite');
          scope.querySelector('.modal-header, .console-section-heading').after(feedback);
        }
        feedback.textContent = '正在处理，请稍候…';
        feedback.dataset.state = 'loading';
        scope.setAttribute('aria-busy', 'true');
        const buttons = [...scope.querySelectorAll('button[type=submit]')].filter(button => !button.disabled);
        buttons.forEach(button => { button.disabled = true; });
        pending.set(scope, (pending.get(scope) || 0) + 1);
        const operation = target[method](...args);
        const finish = (success) => {
          pending.set(scope, pending.get(scope) - 1);
          if (pending.get(scope) === 0) scope.setAttribute('aria-busy', 'false');
          buttons.forEach(button => { button.disabled = false; });
          feedback.textContent = success ? '操作成功' : '操作失败，请检查输入或连接后重试。';
          feedback.dataset.state = success ? 'success' : 'error';
        };
        operation.then(() => finish(true), () => finish(false));
        return operation;
      };
    },
  });
}

export function tablePresentation(tab, title, options, isCurrent) {
  const surface = document.getElementById('vtableContainer');
  document.getElementById('tableFeedback')?.remove();
  const feedback = document.createElement('div');
  feedback.id = 'tableFeedback';
  feedback.className = 'table-feedback';
  feedback.setAttribute('role', 'status');
  feedback.setAttribute('aria-live', 'polite');
  surface.prepend(feedback);
  let loaded = false;
  function setState(state) {
    if (!isCurrent()) return;
    surface.dataset.state = state;
    surface.setAttribute('aria-busy', String(state === 'loading'));
    feedback.replaceChildren();
    feedback.hidden = state === 'ready';
    if (state === 'loading') feedback.textContent = '正在加载' + title + '，请稍候…';
    if (state === 'error') {
      const text = document.createElement('span');
      text.textContent = '加载失败，未能获取最新数据。请检查连接后重试。';
      const retry = document.createElement('button');
      retry.type = 'button';
      retry.className = 'btn btn-secondary';
      retry.textContent = '重新加载';
      retry.addEventListener('click', () => window.vtable.ajax.reload(null, false));
      feedback.append(text, retry);
    }
  }
  setState('loading');
  return {
    ...options,
    ajax(data, callback, settings) {
      // Keep existing rows visible during periodic refreshes; never label stale data as current.
      if (!loaded || surface.dataset.state === 'error') setState('loading');
      // DataTables treats an ajax function return value as jqXHR (with abort()).
      // Observe our fetch promise internally; do not return it to DataTables.
      Promise.resolve(options.ajax.call(this, data, (result) => {
        if (!isCurrent()) return;
        loaded = true;
        setState('ready');
        callback(result);
      }, settings)).catch(() => {
        if (!isCurrent()) return;
        callback({draw: data.draw, recordsTotal: 0, recordsFiltered: 0, data: [], authUser: false});
        setState('error');
      });
    },
    drawCallback(settings) {
      options.drawCallback?.call(this, settings);
      const api = this.api();
      const info = api.page.info();
      const wrapper = api.table().container();
      wrapper.querySelector('.dt-info')?.parentElement.classList.add('console-table-footer');
      wrapper.querySelectorAll('.dt-paging').forEach(el => { el.hidden = info.pages <= 1; });
      wrapper.querySelectorAll('.dt-length, .dt-info').forEach(el => { el.hidden = info.recordsDisplay === 0; });
      const search = wrapper.querySelector('.dt-search input');
      search?.setAttribute('aria-label', '搜索' + title);
      if (api.search() && info.recordsDisplay === 0) {
        const cell = wrapper.querySelector('td.dt-empty');
        if (cell && !cell.querySelector('button')) {
          const clear = document.createElement('button');
          clear.type = 'button';
          clear.className = 'btn btn-link table-clear-search';
          clear.textContent = '清除搜索';
          clear.addEventListener('click', () => { api.search('').draw(); search?.focus(); });
          cell.append(clear);
        }
      }
    },
  };
}
