import React from 'react';
import TelephonyCallsTable from '@/components/telephony/TelephonyCallsTable';

const AdminTelephonyPage: React.FC = () => (
  <TelephonyCallsTable mode="admin" title="Телефония компании" />
);

export default AdminTelephonyPage;
