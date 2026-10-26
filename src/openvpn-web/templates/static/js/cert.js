tables.cert = {
  columns: [
    { title: '名称', data: 'name', render: (data) => escapeHTML(data) },
    { title: '类型', data: 'type', render: (data) => escapeHTML(data) },
    {
      title: '状态',
      data: (data) => {
        let badgeClass = 'text-bg-success';
        if (data.status === '已过期') {
          badgeClass = 'text-bg-danger';
        } else if (data.status === '即将过期') {
          badgeClass = 'text-bg-warning';
        }
        return `<span class="badge ${badgeClass}">${escapeHTML(data.status)}</span>`;
      },
    },
    { title: '颁发时间', data: 'notBefore', render: (data) => escapeHTML(data) },
    { title: '过期时间', data: 'notAfter', render: (data) => escapeHTML(data) },
    { title: '剩余天数', data: 'expiresIn', render: (data) => escapeHTML(data) },
  ],
  order: [[3, 'desc']],
  buttons: {
    dom: {
      button: { className: 'btn btn-sm' },
    },
    buttons: [
      {
        text: '更新证书',
        className: 'btn-primary',
        action: () => $('#renewCertModal').modal('show'),
      },
    ],
  },
  dom:
    "<'d-flex justify-content-between align-items-center'fB>" +
    "<'row'<'col-sm-12'tr>>" +
    "<'d-flex justify-content-between align-items-center'lip>",
  ajax: function (data, callback, settings) {
    return request.get('/ovpn/certs').then((data) => callback({ data }));
  },
};

// 更新证书
$('#renewCertModal form').submit(function () {
  const day = $('#renewCertModal input[name="day"]').val();

  $('#renewCertModal').modal('hide');

  consoleRequest.post('/ovpn/server', { action: 'renewCert', day }).then((data) => {
    message.success(data.message);
    vtable.ajax.reload(null, false);
  });
  return false;
});
