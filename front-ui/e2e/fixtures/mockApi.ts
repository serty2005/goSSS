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

const latestCompanyTicket = {
  id: 'ticket-1003',
  number: 1003,
  subject: 'Проверить связь с агентом кассы',
  description: '<p>Повторная диагностика оборудования.</p>',
  reporter_name: 'Смена ресторана',
  created_source: 'ui',
  status: 'in_progress',
  last_comment: 'Агент прислал новое наблюдение',
  last_comment_author: 'Администратор ServiceDesk',
  last_activity: '2026-04-27T10:10:00Z',
  created_at: '2026-04-27T10:00:00Z',
  company_id: 'company-1',
  company_name: 'Ресторан Север',
  sync_with_bitrix: false,
  assignee: {
    id: 1,
    full_name: 'Администратор ServiceDesk',
  },
};

const globalSearchActiveTicket = {
  id: 'ticket-archive-control-active',
  number: 99001,
  subject: 'архивконтроль: неархивная заявка по кассе',
  description: '<p>архивконтроль: неархивная заявка по кассе для проверки глобального поиска.</p>',
  reporter_name: 'Администратор ServiceDesk',
  created_source: 'ui',
  status: 'new',
  last_comment: 'архивконтроль: комментарий виден в глобальном поиске',
  last_comment_author: 'Администратор ServiceDesk',
  last_activity: '2026-06-02T11:10:00Z',
  created_at: '2026-06-02T10:55:00Z',
  updated_at: '2026-06-02T11:10:00Z',
  company_id: 'company-1',
  company_name: 'Ресторан Север',
  is_archived: false,
  sync_with_bitrix: false,
  assignee: {
    id: 1,
    full_name: 'Администратор ServiceDesk',
  },
};

const globalSearchClosedTicket = {
  id: 'ticket-archive-control-closed',
  number: 99002,
  subject: 'архивконтроль: закрытая, но еще не архивная заявка',
  description: '<p>архивконтроль: закрытая заявка остается в неархивной выдаче.</p>',
  reporter_name: 'Администратор ServiceDesk',
  created_source: 'ui',
  status: 'closed',
  last_comment: '',
  last_comment_author: '',
  last_activity: '2026-06-02T11:20:00Z',
  created_at: '2026-06-02T11:05:00Z',
  updated_at: '2026-06-02T11:20:00Z',
  company_id: 'company-1',
  company_name: 'Ресторан Север',
  is_archived: false,
  sync_with_bitrix: false,
  assignee: {
    id: 1,
    full_name: 'Администратор ServiceDesk',
  },
};

const globalSearchArchivedTicket = {
  id: 'ticket-archive-control-archived',
  number: 99003,
  subject: 'архивконтроль: архивная заявка не должна попадать в глобальный поиск',
  description: '<p>архивконтроль: архивная заявка скрыта из неархивной выдачи.</p>',
  reporter_name: 'Администратор ServiceDesk',
  created_source: 'ui',
  status: 'closed',
  last_comment: '',
  last_comment_author: '',
  last_activity: '2026-06-02T11:30:00Z',
  created_at: '2026-06-02T11:15:00Z',
  updated_at: '2026-06-02T11:30:00Z',
  company_id: 'company-1',
  company_name: 'Ресторан Север',
  is_archived: true,
  sync_with_bitrix: false,
  assignee: {
    id: 1,
    full_name: 'Администратор ServiceDesk',
  },
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

const networkChildren = [
  {
    id: 'company-child-1',
    title: 'Ресторан Север Бар',
    additional_name: 'ООО Север Бар',
    address: 'Москва, ул. Сервисная, 12',
    parent_id: 'company-1',
    parent_title: 'Ресторан Север',
    active_contract: true,
    contract_id: 'contract-1',
    contract_type: 'TS Standart',
  },
  {
    id: 'company-child-2',
    title: 'Ресторан Север Доставка',
    additional_name: 'ООО Север Доставка',
    address: 'Москва, ул. Сервисная, 14',
    parent_id: 'company-1',
    parent_title: 'Ресторан Север',
    active_contract: true,
    contract_id: 'contract-1',
    contract_type: 'TS Standart',
  },
  {
    id: 'company-child-3',
    title: 'Ресторан Север Склад',
    additional_name: 'ООО Север Склад',
    address: 'Москва, ул. Складская, 4',
    parent_id: 'company-1',
    parent_title: 'Ресторан Север',
    active_contract: false,
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

const ticketMatchesSearch = (ticket: Record<string, unknown>, rawSearch: string) => {
  const search = rawSearch.trim().toLowerCase();
  if (!search) {
    return true;
  }
  return [
    ticket.number,
    ticket.subject,
    ticket.description,
    ticket.last_comment,
    ticket.company_name,
  ].some((value) => String(value || '').toLowerCase().includes(search));
};

const toSearchTicket = (ticket: typeof ticketList[number] | typeof latestCompanyTicket | typeof globalSearchActiveTicket) => ({
  id: ticket.id,
  number: ticket.number,
  subject: ticket.subject,
  description: ticket.description,
  status: ticket.status,
  company_id: ticket.company_id,
  company_name: ticket.company_name,
  assignee_name: ticket.assignee?.full_name || '',
  reporter_name: ticket.reporter_name,
  last_comment: ticket.last_comment,
  last_activity: ticket.last_activity,
  created_at: ticket.created_at,
  updated_at: 'updated_at' in ticket ? ticket.updated_at : ticket.last_activity,
  is_archived: 'is_archived' in ticket ? ticket.is_archived : false,
  created_source: ticket.created_source,
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
      const companyID = url.searchParams.get('company_id') || '';
      const companyIDs = (url.searchParams.get('company_ids') || '')
        .split(',')
        .map((item) => item.trim())
        .filter(Boolean);
      const search = url.searchParams.get('search') || '';
      if (search.trim()) {
        const items = [
          ...ticketList,
          latestCompanyTicket,
          globalSearchActiveTicket,
          globalSearchClosedTicket,
        ].filter((ticket) => ticketMatchesSearch(ticket, search));
        await route.fulfill(json(ok(items, {
          total: items.length,
          limit: Number(url.searchParams.get('limit') || 20),
          offset: Number(url.searchParams.get('offset') || 0),
          has_next: false,
        })));
        return;
      }
      if (companyID === 'company-1' || companyIDs.includes('company-1')) {
        const items = [ticketList[0], latestCompanyTicket];
        await route.fulfill(json(ok(items, {
          total: items.length,
          limit: Number(url.searchParams.get('limit') || 20),
          offset: Number(url.searchParams.get('offset') || 0),
          has_next: false,
        })));
        return;
      }

      await route.fulfill(json(ok(ticketList, {
        total: ticketList.length,
        limit: Number(url.searchParams.get('limit') || 20),
        offset: Number(url.searchParams.get('offset') || 0),
        has_next: false,
      })));
      return;
    }

    if (method === 'GET' && path === '/search') {
      const term = url.searchParams.get('term') || '';
      const matchedTickets = [
        globalSearchActiveTicket,
        globalSearchClosedTicket,
        globalSearchArchivedTicket,
      ]
        .filter((ticket) => !ticket.is_archived && ticketMatchesSearch(ticket, term))
        .map(toSearchTicket);

      await route.fulfill(json(ok({
        search_results: matchedTickets.length > 0
          ? [
              {
                owner: {
                  uuid: 'company-1',
                  external_uuid: null,
                  name: 'Ресторан Север',
                  address: 'Москва, ул. Сервисная, 10',
                  active_contract: true,
                },
                found_entities: [],
                matched_tickets: matchedTickets,
                active_tickets: [toSearchTicket(latestCompanyTicket)],
              },
            ]
          : [],
        ticket_results_without_company: [],
      })));
      return;
    }

    if (method === 'POST' && path === '/tickets') {
      const body = await readJsonBody(route);
      await route.fulfill(json(ok({
        ...ticketList[0],
        id: 'ticket-created-e2e',
        number: 1101,
        subject: String(body.subject || 'Новая заявка E2E'),
        description: String(body.description || ''),
        company_id: String(body.company_id || 'company-1'),
        assignee: {
          id: Number(body.assignee_id || 1),
          full_name: 'Администратор ServiceDesk',
        },
      })));
      return;
    }

    if (method === 'GET' && path === '/tickets/ticket-1001') {
      await route.fulfill(json(ok(ticketDetails)));
      return;
    }

    if (method === 'PATCH' && (path === '/telephony/tickets/ticket-created-e2e/contact' || path === '/telephony/tickets/ticket-1001/contact')) {
      await route.fulfill(json(ok({ status: 'ok' })));
      return;
    }

    if (method === 'GET' && path === '/companies/company-1') {
      await route.fulfill(json(ok({
        id: 'company-1',
        title: 'Ресторан Север',
        additional_name: '',
        address: 'Москва, ул. Сервисная, 10',
        active_contract: true,
        contract_id: 'contract-1',
        contract_type: 'TS Standart',
      })));
      return;
    }

    if (method === 'GET' && path === '/companies/company-1/children') {
      await route.fulfill(json(ok(networkChildren)));
      return;
    }

    const networkChildChildrenPath = networkChildren.find((item) => method === 'GET' && path === `/companies/${item.id}/children`);
    if (networkChildChildrenPath) {
      await route.fulfill(json(ok([])));
      return;
    }

    const networkChild = networkChildren.find((item) => method === 'GET' && path === `/companies/${item.id}`);
    if (networkChild) {
      await route.fulfill(json(ok(networkChild)));
      return;
    }

    const networkChildByInfrastructurePath = networkChildren.find((item) => method === 'GET' && path === `/companies/${item.id}/infrastructure`);
    if (networkChildByInfrastructurePath) {
      await route.fulfill(json(ok([
        {
          entity_type: 'Server',
          data: {
            uuid: `server-${networkChildByInfrastructurePath.id}`,
            server_name: `srv-${networkChildByInfrastructurePath.id}`,
            ip: '10.10.2.10:443',
            server_version: '2026.4',
            crm_id: `crm-${networkChildByInfrastructurePath.id}`,
            unique_id: `uid-${networkChildByInfrastructurePath.id}`,
          },
        },
      ])));
      return;
    }

    if (method === 'GET' && path === '/companies/company-1/infrastructure') {
      await route.fulfill(json(ok([
        {
          entity_type: 'Server',
          data: {
            uuid: 'server-1',
            server_name: 'srv-rest-sever',
            ip: '10.10.1.10:443',
            anydesk: '123 456 789',
            rdp: '10.10.1.10',
            crm_id: 'CRM-501',
            server_version: '2026.4',
            unique_id: 'UID-SRV-001',
            partners_link: 'https://partners.example.test/server-1',
            iiko_web_link: 'https://demo.syrve.app/',
          },
        },
        {
          entity_type: 'Workstation',
          data: {
            uuid: 'workstation-1',
            device_name: 'Касса 1',
            anydesk: '111 222 333',
            teamviewer: 'tv-444',
            litemanager: 'lm-555',
            rustdesk: 'rd-666',
            last_updated_by: 'agent-aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee',
            last_modified_date: '2026-04-27T10:05:00Z',
          },
        },
        {
          entity_type: 'FiscalRegister',
          data: {
            uuid: 'fiscal-1',
            model_kkt: 'АТОЛ 55Ф',
            serial_number: 'SN123456789',
            rn_kkt: '0001234567890123',
            legal_name: 'ООО Север',
            inn: '7700000000',
            address: 'Москва, ул. Сервисная, дом 10, помещение 1, кассовая зона',
            last_updated_by: 'agent-aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee',
            last_modified_date: '2026-04-27T10:05:00Z',
          },
        },
        {
          entity_type: 'Workstation',
          data: {
            uuid: 'workstation-uuid-agent',
            device_name: 'Касса с агентским UUID',
            anydesk: '777 888 999',
            last_updated_by: '56de9576-b988-d422-e8d3-3264ae6a5409',
            last_modified_date: '2026-05-01T00:12:13+03:00',
          },
        },
        {
          entity_type: 'FiscalRegister',
          data: {
            uuid: 'fiscal-uuid-agent',
            model_kkt: 'АТОЛ UUID',
            serial_number: 'SNUUIDAGENT',
            rn_kkt: '0001112223334445',
            legal_name: 'ООО Север',
            inn: '7700000000',
            address: 'Москва, ул. Сервисная, дом 10, помещение UUID',
            last_updated_by: '56de9576-b988-d422-e8d3-3264ae6a5409',
            last_modified_date: '2026-05-01T00:12:13+03:00',
          },
        },
        {
          entity_type: 'Workstation',
          data: {
            uuid: 'workstation-agent-type-only',
            device_name: 'Фастфуд 4 (Пицца 2)',
            anydesk: '621466312',
            teamviewer: '1817149145',
            last_updated_by: 'agent',
            last_modified_date: '2026-05-01T00:12:13+03:00',
          },
        },
        {
          entity_type: 'FiscalRegister',
          data: {
            uuid: 'fiscal-agent-type-only',
            model_kkt: 'ШТРИХ-М-01Ф',
            serial_number: 'FRWITHOUTAGENTID',
            rn_kkt: '0009876543210000',
            legal_name: 'ООО Север',
            inn: '7700000000',
            address: 'Москва, ул. Сервисная, дом 10, помещение 2',
            last_updated_by: 'agent',
            last_modified_date: '2026-05-01T00:12:13+03:00',
          },
        },
      ])));
      return;
    }

    if (method === 'GET' && path === '/agent-observations') {
      const agentUUID = url.searchParams.get('agent_uuid') || '';
      const workstationID = url.searchParams.get('workstation_id') || '';
      const frID = url.searchParams.get('fr_id') || '';
      const shouldReturnObservation = agentUUID === 'agent-aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee'
        || workstationID === 'workstation-uuid-agent'
        || workstationID === 'workstation-1'
        || frID === 'fiscal-uuid-agent'
        || frID === 'fiscal-1';
      await route.fulfill(json(ok(shouldReturnObservation ? [
          {
            observation_id: 9001,
            agent_uuid: agentUUID || 'agent-aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee',
            vc: '1.8.0',
            workstation_id: workstationID || 'workstation-1',
            workstation_name: workstationID === 'workstation-uuid-agent' ? 'Касса с агентским UUID' : 'Касса 1',
            fr_id: frID || 'fiscal-1',
            fr_name: frID === 'fiscal-uuid-agent' ? 'АТОЛ UUID' : 'АТОЛ 55Ф',
            owner_match: true,
            observed_at: '2026-04-27T10:05:00Z',
            current_time: '2026-04-27T13:05:00+03:00',
            v_time: '2026-04-27T13:05:00+03:00',
            server_url: 'https://srv-rest-sever.example.test',
          },
        ] : [])));
      return;
    }

    if (method === 'GET' && path === '/agent-observations/9001') {
      await route.fulfill(json(ok({
        id: 9001,
        status: 'processed',
        observed_at: '2026-04-27T10:05:00Z',
        source: 'agent',
        payload_json: {
          agent_uuid: 'agent-aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee',
          workstation: 'Касса 1',
          fiscal_register: 'АТОЛ 55Ф',
        },
      })));
      return;
    }

    if (method === 'GET' && path === '/companies') {
      const parentIDs = (url.searchParams.get('parent_ids') || '')
        .split(',')
        .map((item) => item.trim())
        .filter(Boolean);
      const items = parentIDs.length
        ? [...companyList, ...networkChildren].filter((item) => parentIDs.includes(String(item.parent_id || '')))
        : companyList;
      await route.fulfill(json(ok(items, {
        total: items.length,
        limit: Number(url.searchParams.get('limit') || 20),
        offset: Number(url.searchParams.get('offset') || 0),
        has_next: false,
      })));
      return;
    }

    if (method === 'GET' && path === '/companies/parents') {
      await route.fulfill(json(ok([
        { id: 'company-1', title: 'Ресторан Север', children_count: networkChildren.length },
      ])));
      return;
    }

    if (
      method === 'GET' &&
      (path === '/companies/with-bitrix-service-point-mappings' ||
        path === '/companies/bitrix-service-point-mappings')
    ) {
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
