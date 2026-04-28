import type { Page, Route } from '@playwright/test';

type MockApiOptions = {
  failNextProfileConfigPatch?: boolean;
};

const adminUser = {
  id: 1,
  username: 'admin',
  full_name: 'Администратор ServiceDesk',
  first_name: 'Админ',
  last_name: 'ServiceDesk',
  position: 'admin',
  email: 'admin@example.test',
  roles: ['admin', 'support_specialist'],
  bitrix_enabled: true,
  pyrus_enabled: true,
  schedule_type: '5/2',
  is_active: true,
  has_logged_in: true,
  integrations: [],
  profile_config: {
    interface: {
      locale: 'ru',
      theme_mode: 'light',
      search: {
        cards_columns: 4,
      },
    },
    notifications: {
      personal_enabled: true,
      common_enabled: true,
      common_ticket_updates: true,
      common_comments: true,
      common_deferred_due: true,
    },
    tickets: {
      comments_new_first: true,
      subscriptions: ['ticket-1001'],
      filters: {
        presets: [],
      },
    },
  },
};

const ticketList = [
  {
    id: 'ticket-1001',
    number: 1001,
    subject: 'Касса не печатает чек',
    description: '<p>Касса не печатает чек после обновления смены.</p>',
    reporter_name: 'Ирина кассир',
    created_source: 'ui',
    status: 'new',
    last_comment: 'Проверить доступность фискального регистратора',
    last_comment_author: 'Администратор ServiceDesk',
    last_activity: '2026-04-27T08:15:00Z',
    created_at: '2026-04-27T07:40:00Z',
    company_id: 'company-1',
    company_name: 'Ресторан Север',
    contact_id: 21,
    contract_id: 'contract-1',
    is_common_contract: true,
    sync_with_bitrix: true,
    bitrix_service_point_id: 501,
    bitrix_deal_title: 'Ресторан Север / касса',
    bitrix_deal_id: 901,
    bitrix_deal_url: 'https://bitrix.example.test/crm/deal/details/901/',
    pyrus_task_id: 7001,
    pyrus_task_url: 'https://pyrus.example.test/t#id7001',
    assignee: {
      id: 1,
      full_name: 'Администратор ServiceDesk',
    },
  },
  {
    id: 'ticket-1002',
    number: 1002,
    subject: 'Нет доступа к RDP',
    description: '<p>У сотрудника нет доступа к RDP на рабочую станцию.</p>',
    reporter_name: 'Олег менеджер',
    created_source: 'servicedesk',
    status: 'in_progress',
    last_comment: 'Запрошены данные подключения',
    last_comment_author: 'Линия поддержки',
    last_activity: '2026-04-27T09:20:00Z',
    created_at: '2026-04-27T08:55:00Z',
    company_id: 'company-2',
    company_name: 'Кафе Восток',
    sync_with_bitrix: false,
    assignee: {
      id: 2,
      full_name: 'Мария оператор',
    },
  },
];

const ticketDetails = {
  metadata: {
    ...ticketList[0],
    updated_at: ticketList[0].last_activity,
    priority: 'normal',
  },
  company_name: 'Ресторан Север',
  contact: {
    id: 21,
    phone_normalized: '+79990000001',
    phone_display: '+7 999 000-00-01',
    name: 'Ирина кассир',
  },
  calls: [],
  comments: [
    {
      uuid: 'comment-1',
      author_name: 'Администратор ServiceDesk',
      creation_date: '2026-04-27T08:16:00Z',
      text: 'Проверить доступность фискального регистратора',
      is_private: false,
    },
  ],
  history: [],
  attachments: [],
};

const companyList = [
  {
    id: 'company-1',
    title: 'Ресторан Север',
    additional_name: 'ООО Север',
    address: 'Москва, ул. Сервисная, 10',
    active_contract: true,
  },
  {
    id: 'company-2',
    title: 'Кафе Восток',
    additional_name: 'ООО Восток',
    address: 'Москва, пр-т Техподдержки, 7',
    active_contract: true,
  },
];

const companyMappings = [
  {
    company_id: 'company-1',
    company_title: 'Ресторан Север',
    company_parent_title: '',
    bitrix_service_point_id: 501,
    bitrix_service_point_name: 'Ресторан Север / касса',
    bitrix_service_point_address: 'Москва, ул. Сервисная, 10',
    bitrix_service_point_contract_on: true,
  },
];

const bitrixServicePoints = [
  {
    b24_element_id: 501,
    name: 'Ресторан Север / касса',
    address: 'Москва, ул. Сервисная, 10',
    one_c_code: 'RS-001',
    contract_on: true,
  },
];

const serverList = [
  {
    id: 'server-1',
    unique_id: 'server-1',
    server_name: 'srv-rest-sever',
    device_name: 'srv-rest-sever',
    ip: '10.10.1.10',
    server_version: '2026.4',
    status: 'online',
    owner_id: 'company-1',
    owner_title: 'Ресторан Север',
    owner_parent_id: '',
    owner_parent_title: '',
    server_type: 'POS',
  },
  {
    id: 'server-2',
    unique_id: 'server-2',
    server_name: 'srv-cafe-vostok',
    device_name: 'srv-cafe-vostok',
    ip: '10.20.1.10',
    server_version: '2026.4',
    status: 'warning',
    owner_id: 'company-2',
    owner_title: 'Кафе Восток',
    owner_parent_id: '',
    owner_parent_title: '',
    server_type: 'BackOffice',
  },
];

const userList = [
  {
    id: 1,
    username: 'admin',
    full_name: 'Администратор ServiceDesk',
    first_name: 'Админ',
    last_name: 'ServiceDesk',
    position: 'admin',
    email: 'admin@example.test',
    schedule_type: '5/2',
    is_active: true,
    has_logged_in: true,
    bitrix_enabled: true,
    pyrus_enabled: true,
    roles: ['admin', 'support_specialist'],
    integrations: [
      {
        id: 1,
        integration_type: 'bitrix24',
        external_id: '1',
        is_enabled: true,
        is_verified: true,
        is_locked: false,
        verified_name: 'Администратор ServiceDesk',
      },
    ],
  },
  {
    id: 2,
    username: 'maria',
    full_name: 'Мария оператор',
    first_name: 'Мария',
    last_name: 'Оператор',
    position: 'support_specialist',
    email: 'maria@example.test',
    schedule_type: '2/2',
    is_active: true,
    has_logged_in: true,
    bitrix_enabled: false,
    pyrus_enabled: false,
    roles: ['support_specialist'],
    integrations: [],
  },
];

const json = (data: unknown, status = 200) => ({
  status,
  contentType: 'application/json',
  body: JSON.stringify(data),
});

const ok = (data: unknown, meta?: Record<string, unknown>) => ({
  status: 'success',
  data,
  ...(meta ? { meta } : {}),
});

const readJsonBody = async (route: Route) => {
  const postData = route.request().postData();
  if (!postData) {
    return {};
  }

  try {
    return JSON.parse(postData) as Record<string, unknown>;
  } catch {
    return {};
  }
};

export const installMockApi = async (page: Page, options: MockApiOptions = {}) => {
  let shouldFailNextProfileConfigPatch = Boolean(options.failNextProfileConfigPatch);

  await page.route('**/api/**', async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (!url.pathname.startsWith('/api/')) {
      await route.continue();
      return;
    }
    const path = url.pathname.replace(/^\/api/, '');
    const method = request.method().toUpperCase();

    if (method === 'POST' && path === '/auth/login') {
      const body = await readJsonBody(route);
      if (body.username === 'admin' && body.password === 'admin') {
        await route.fulfill(json(ok({ access_token: 'e2e-admin-token', user: adminUser })));
        return;
      }

      await route.fulfill(json({
        status: 'error',
        error: { error: 'Неверное имя пользователя или пароль' },
      }, 401));
      return;
    }

    if (method === 'GET' && path === '/events') {
      await route.fulfill({
        status: 200,
        contentType: 'text/event-stream',
        body: ': e2e stream stub\n\n',
      });
      return;
    }

    if (method === 'GET' && path === '/translations') {
      await route.fulfill(json(ok(null)));
      return;
    }

    if (method === 'GET' && path === '/profile/me') {
      await route.fulfill(json(ok(adminUser)));
      return;
    }

    if (method === 'GET' && path === '/profile/config') {
      await route.fulfill(json(ok({ profile_config: adminUser.profile_config })));
      return;
    }

    if (method === 'PATCH' && path === '/profile/config') {
      if (shouldFailNextProfileConfigPatch) {
        shouldFailNextProfileConfigPatch = false;
        await route.fulfill(json({
          status: 'error',
          error: { error: 'E2E: сохранение оформления недоступно' },
        }, 500));
        return;
      }

      const body = await readJsonBody(route);
      await route.fulfill(json(ok({ ...adminUser, profile_config: body.profile_config || adminUser.profile_config })));
      return;
    }

    if (method === 'GET' && path === '/profile/assignees') {
      await route.fulfill(json(ok([
        { id: 1, full_name: 'Администратор ServiceDesk', username: 'admin', is_active: true },
        { id: 2, full_name: 'Мария оператор', username: 'maria', is_active: true },
      ])));
      return;
    }

    if (method === 'GET' && path === '/telephony/line') {
      await route.fulfill(json(ok({
        provider: 'megafon',
        status: 'online',
        employees: [
          { user_id: 1, login: 'admin', name: 'Администратор ServiceDesk', status: 'online', provider: 'megafon' },
        ],
      })));
      return;
    }

    if (method === 'GET' && path === '/telephony/pending-context/me') {
      await route.fulfill(json(ok(null)));
      return;
    }

    if (method === 'GET' && path === '/tickets/stats/dashboard') {
      await route.fulfill(json(ok({
        total_tickets: 2,
        accepted_calls_24h: 4,
        polled_servers_24h: 7,
        resolved_by_assignee: [
          { user_id: 1, user_name: 'Администратор ServiceDesk', today_count: 1, days_7_count: 3, days_30_count: 8 },
        ],
        accepted_calls_by_employee: [
          { user_id: 1, user_name: 'Администратор ServiceDesk', count: 4 },
        ],
        server_statuses: [
          { status: 'online', count: 6 },
          { status: 'warning', count: 1 },
        ],
      })));
      return;
    }

    if (method === 'GET' && path === '/tickets/filters') {
      await route.fulfill(json(ok({
        companies: [
          { id: 'company-1', name: 'Ресторан Север', count: 1 },
          { id: 'company-2', name: 'Кафе Восток', count: 1 },
        ],
      })));
      return;
    }

    if (method === 'GET' && path === '/tickets') {
      await route.fulfill(json(ok(ticketList, {
        total: ticketList.length,
        limit: Number(url.searchParams.get('limit') || 20),
        offset: Number(url.searchParams.get('offset') || 0),
        has_next: false,
      })));
      return;
    }

    if (method === 'GET' && path === '/tickets/ticket-1001') {
      await route.fulfill(json(ok(ticketDetails)));
      return;
    }

    if (method === 'GET' && path === '/companies/company-1') {
      await route.fulfill(json(ok({
        id: 'company-1',
        title: 'Ресторан Север',
        additional_name: '',
      })));
      return;
    }

    if (method === 'GET' && path === '/companies/company-1/infrastructure') {
      await route.fulfill(json(ok([
        {
          entity_type: 'Server',
          data: {
            uuid: 'server-1',
            server_name: 'srv-rest-sever',
            ip: '10.10.1.10',
            anydesk: '123 456 789',
            rdp: '10.10.1.10',
          },
        },
      ])));
      return;
    }

    if (method === 'GET' && path === '/companies') {
      await route.fulfill(json(ok(companyList, {
        total: companyList.length,
        limit: Number(url.searchParams.get('limit') || 20),
        offset: Number(url.searchParams.get('offset') || 0),
        has_next: false,
      })));
      return;
    }

    if (method === 'GET' && path === '/companies/bitrix-service-point-mappings') {
      await route.fulfill(json(ok(companyMappings, {
        total: companyMappings.length,
        limit: Number(url.searchParams.get('limit') || 50),
        offset: Number(url.searchParams.get('offset') || 0),
        has_next: false,
      })));
      return;
    }

    if (method === 'GET' && path === '/bitrix/service-points') {
      await route.fulfill(json(ok(bitrixServicePoints)));
      return;
    }

    if (method === 'GET' && path === '/servers') {
      await route.fulfill(json(ok(serverList, {
        total: serverList.length,
        limit: Number(url.searchParams.get('limit') || 20),
        offset: Number(url.searchParams.get('offset') || 0),
        has_next: false,
      })));
      return;
    }

    if (method === 'GET' && path === '/users') {
      await route.fulfill(json(ok(userList)));
      return;
    }

    await route.fulfill(json(ok(null)));
  });
};
