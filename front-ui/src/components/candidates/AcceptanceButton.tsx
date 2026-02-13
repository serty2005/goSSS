import React from 'react';
import { Button, Tooltip } from 'antd';

interface AcceptanceButtonProps {
  isBlocked: boolean;
  blockReasons: string[];
  onSubmit: () => void;
  isPending: boolean;
  className?: string;
}

export const AcceptanceButton: React.FC<AcceptanceButtonProps> = ({
  isBlocked,
  blockReasons,
  onSubmit,
  isPending,
  className
}) => {
  const buttonText = isBlocked ? 'Невозможно подтвердить' : 'Подтвердить принятие';
  const tooltipTitle = isBlocked ? blockReasons.join('\n') : 'Нажмите для подтверждения';

  return (
    <Tooltip title={tooltipTitle}>
      <span>
        <Button
          type="primary"
          loading={isPending}
          disabled={isBlocked}
          onClick={onSubmit}
          className={className}
        >
          {buttonText}
        </Button>
      </span>
    </Tooltip>
  );
};