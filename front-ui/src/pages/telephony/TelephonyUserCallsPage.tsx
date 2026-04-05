import React, { useMemo } from 'react';
import { Navigate, useParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import TelephonyCallsTable from '@/components/telephony/TelephonyCallsTable';
import { usersApi } from '@/api/users';
import { useAuthStore } from '@/store/authStore';

const TelephonyUserCallsPage: React.FC = () => {
  const { id } = useParams();
  const user = useAuthStore((state) => state.user);
  const userId = Number(id || 0);
  const { data: assigneesResponse } = useQuery({
    queryKey: ['users-assignees'],
    queryFn: () => usersApi.getAssignees(),
    enabled: userId > 0 && user?.id !== userId,
    staleTime: 60_000,
  });

  const title = useMemo(() => {
    if (user?.id === userId) {
      return 'Мои звонки';
    }
    const match = assigneesResponse?.data?.find((item) => item.id === userId);
    if (match) {
      return `Звонки сотрудника: ${match.full_name || match.username}`;
    }
    return `Звонки сотрудника #${userId}`;
  }, [assigneesResponse?.data, user?.id, userId]);

  if (!userId) {
    return <Navigate to="/tickets" replace />;
  }

  return <TelephonyCallsTable mode="user" userId={userId} title={title} />;
};

export default TelephonyUserCallsPage;
