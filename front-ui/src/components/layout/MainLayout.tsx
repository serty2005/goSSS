import React, { useState } from 'react';
import { Layout, Menu, Button, Dropdown, Avatar, theme as antTheme, Typography, Space } from 'antd';
import { Outlet, useNavigate, useLocation } from 'react-router-dom';
import {
  SearchOutlined,
  CheckSquareOutlined,
  CustomerServiceOutlined,
  BankOutlined,
  DesktopOutlined,
  SettingOutlined,
  UserOutlined,
  LogoutOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  SunOutlined,
  MoonOutlined
} from '@ant-design/icons';
import HeaderSearch from '@/components/common/HeaderSearch';
import { useUiStore } from '@/store/uiStore';
import { useAuthStore } from '@/store/authStore';

const { Header, Sider, Content } = Layout;
const { Text } = Typography;

const MainLayout: React.FC = () => {
  const [collapsed, setCollapsed] = useState(false);
  const navigate = useNavigate();
  const location = useLocation();
  const { token } = antTheme.useToken();
  
  const { themeMode, toggleTheme } = useUiStore();
  const { user, logout } = useAuthStore();

  const handleMenuClick = (key: string) => {
    navigate(key);
  };

  const handleLogout = () => {
    logout();
    navigate('/login');
  };

  const userMenu = {
    items: [
      {
        key: 'profile',
        label: 'Профиль',
        icon: <UserOutlined />,
      },
      {
        key: 'logout',
        label: 'Выйти',
        icon: <LogoutOutlined />,
        onClick: handleLogout,
      },
    ],
  };

  const menuItems = [
    { key: '/', icon: <SearchOutlined />, label: 'Поиск' },
    { key: '/tasks', icon: <CheckSquareOutlined />, label: 'Задачи' },
    { key: '/tickets', icon: <CustomerServiceOutlined />, label: 'Тикеты' },
    { key: '/companies', icon: <BankOutlined />, label: 'Компании' },
    {
      key: 'equipment',
      icon: <DesktopOutlined />,
      label: 'Оборудование',
      children: [
        { key: '/servers', label: 'Серверы' },
        { key: '/workstations', label: 'Рабочие станции' },
        { key: '/fiscals', label: 'ФР' },
      ],
    },
    { key: '/admin', icon: <SettingOutlined />, label: 'Администрирование' },
  ];

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider
        trigger={null}
        collapsible
        collapsed={collapsed}
        width={250}
        style={{
          background: token.colorBgContainer,
          borderRight: `1px solid ${token.colorBorderSecondary}`,
          backdropFilter: 'blur(10px)',
        }}
      >
        <div style={{ height: 64, margin: 16, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
          {/* Логотип заглушка */}
          <div style={{ 
            width: collapsed ? 32 : '100%', 
            height: 32, 
            background: token.colorPrimary, 
            borderRadius: 6,
            opacity: 0.8,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            color: '#fff',
            fontWeight: 'bold'
          }}>
            {collapsed ? 'ES' : 'Etalon ServiceDesk'}
          </div>
        </div>
        <Menu
          mode="inline"
          defaultSelectedKeys={[location.pathname]}
          items={menuItems}
          onClick={({ key }) => handleMenuClick(key)}
          style={{ borderRight: 0, background: 'transparent' }}
        />
      </Sider>
      <Layout>
        <Header style={{ 
          padding: '0 24px', 
          background: token.colorBgContainer,
          backdropFilter: 'blur(10px)',
          display: 'flex', 
          alignItems: 'center', 
          justifyContent: 'space-between', // Важно: разносим элементы
          borderBottom: `1px solid ${token.colorBorderSecondary}`,
          position: 'sticky',
          top: 0,
          zIndex: 10
        }}>
          <div style={{ display: 'flex', alignItems: 'center' }}>
            <Button
              type="text"
              icon={collapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
              onClick={() => setCollapsed(!collapsed)}
              style={{ fontSize: '16px', width: 64, height: 64 }}
            />
          </div>
          {/* ЦЕНТР: Глобальный поиск */}
          <div style={{ flex: 1, display: 'flex', justifyContent: 'center' }}>
             <HeaderSearch />
          </div>

          <Space size="middle">
            <Button 
              shape="circle" 
              icon={themeMode === 'light' ? <MoonOutlined /> : <SunOutlined />} 
              onClick={toggleTheme} 
            />
            
            <Dropdown menu={userMenu} placement="bottomRight" arrow>
              <Space style={{ cursor: 'pointer' }}>
                <Avatar style={{ backgroundColor: token.colorPrimary }}>
                  {user?.fullName?.[0] || 'A'}
                </Avatar>
                <Text>{user?.fullName || 'User'}</Text>
              </Space>
            </Dropdown>
          </Space>
        </Header>
        <Content
          style={{
            margin: '24px 16px',
            padding: 24,
            minHeight: 280,
            overflow: 'initial',
            // Контент будет рендериться здесь
          }}
        >
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  );
};

export default MainLayout;