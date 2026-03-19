import { useCallback } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';

type BackNavigationState = {
  backTo?: string;
};

export const useBackNavigation = (fallbackPath = '/') => {
  const navigate = useNavigate();
  const location = useLocation();

  return useCallback(() => {
    const backTo = (location.state as BackNavigationState | null)?.backTo;
    if (typeof backTo === 'string' && backTo.trim()) {
      navigate(backTo);
      return;
    }

    if (window.history.length > 1) {
      navigate(-1);
      return;
    }

    navigate(fallbackPath);
  }, [fallbackPath, location.state, navigate]);
};
