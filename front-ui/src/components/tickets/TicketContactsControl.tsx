import React, { useEffect, useMemo, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Button, Checkbox, Divider, Input, Modal, Radio, Space, Tag, Tooltip, Typography, message } from 'antd';
import { CopyOutlined, DeleteOutlined, PlusOutlined, SaveOutlined, StarFilled } from '@ant-design/icons';
import { telephonyApi } from '@/api/telephony';
import type { TelephonyContactDTO, TicketContactDTO, TicketContactType } from '@/types/api';
import { getTelephonyContactPhoneForCopy } from '@/utils/telephony';

const { Text } = Typography;

type TicketContactsControlProps = {
  ticketId?: string;
  contacts?: TicketContactDTO[];
  legacyContact?: TelephonyContactDTO | null;
  disabled?: boolean;
  highlightStyle?: React.CSSProperties;
  onChanged?: () => void;
};

const buildLegacyContact = (contact?: TelephonyContactDTO | null): TicketContactDTO | null => {
  if (!contact) return null;
  const value = contact.phone_normalized || contact.phone_display;
  if (!value) return null;
  return {
    id: 0,
    contact_type: 'phone',
    telephony_contact_id: contact.id,
    value,
    display_value: contact.phone_display || contact.phone_normalized,
    name: contact.name,
    is_primary: true,
    primary_mode: 'auto',
    source: 'legacy',
    telephony_contact: contact,
  };
};

const normalizeContactType = (value?: string): TicketContactType => (value === 'telegram' ? 'telegram' : 'phone');

const getContactValue = (item?: TicketContactDTO | null) => {
  if (!item) return '';
  if (normalizeContactType(item.contact_type) === 'phone') {
    return getTelephonyContactPhoneForCopy(item.telephony_contact, item.value || item.display_value);
  }
  return item.display_value || item.value;
};

const getContactName = (item?: TicketContactDTO | null) =>
  String(item?.name || item?.telephony_contact?.name || '').trim();

const getContactTitle = (item?: TicketContactDTO | null) => {
  if (!item) return '';
  const name = getContactName(item);
  const value = String(item.display_value || item.value || getContactValue(item)).trim();
  if (name && value && name !== value) {
    return `${name} (${value})`;
  }
  return name || value;
};

const getContactSubtitle = (item: TicketContactDTO) => {
  const value = String(item.display_value || item.value || '').trim();
  const copyValue = getContactValue(item);
  return value || copyValue;
};

const extractErrorText = (error: unknown, fallback: string) => {
  const payload = error as { response?: { data?: { error?: { error?: string } } }; message?: string } | undefined;
  return payload?.response?.data?.error?.error || payload?.message || fallback;
};

const TicketContactsControl: React.FC<TicketContactsControlProps> = ({
  ticketId,
  contacts,
  legacyContact,
  disabled,
  highlightStyle,
  onChanged,
}) => {
  const queryClient = useQueryClient();
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [selectedContactId, setSelectedContactId] = useState<number | null>(null);
  const [contactType, setContactType] = useState<TicketContactType>('phone');
  const [contactValue, setContactValue] = useState('');
  const [contactName, setContactName] = useState('');
  const [makePrimary, setMakePrimary] = useState(false);

  const normalizedContacts = useMemo(() => {
    const list = (contacts || []).filter((item) => item && (item.value || item.display_value));
    if (list.length > 0) return list;
    const legacy = buildLegacyContact(legacyContact);
    return legacy ? [legacy] : [];
  }, [contacts, legacyContact]);

  const primaryContact = useMemo(
    () => normalizedContacts.find((item) => item.is_primary) || normalizedContacts[0],
    [normalizedContacts],
  );
  const selectedContact = useMemo(
    () => normalizedContacts.find((item) => item.id > 0 && item.id === selectedContactId) || null,
    [normalizedContacts, selectedContactId],
  );
  const extraCount = Math.max(normalizedContacts.length - 1, 0);
  const primaryValue = getContactValue(primaryContact);

  const fillFormFromContact = (item?: TicketContactDTO | null) => {
    if (!item || item.id === 0) {
      setSelectedContactId(null);
      setContactType('phone');
      setContactValue('');
      setContactName('');
      setMakePrimary(false);
      return;
    }
    setSelectedContactId(item.id);
    setContactType(normalizeContactType(item.contact_type));
    setContactValue(getContactValue(item));
    setContactName(getContactName(item));
    setMakePrimary(item.is_primary);
  };

  const openContactsModal = () => {
    fillFormFromContact(primaryContact);
    setIsModalOpen(true);
  };

  useEffect(() => {
    if (!isModalOpen || selectedContactId === null) return;
    const exists = normalizedContacts.some((item) => item.id === selectedContactId);
    if (!exists) {
      fillFormFromContact(null);
    }
  }, [isModalOpen, normalizedContacts, selectedContactId]);

  const isEditingExisting = Boolean(selectedContact && selectedContact.id > 0);
  const selectedContactType = normalizeContactType(selectedContact?.contact_type);
  const hasContactChanges =
    isEditingExisting &&
    (contactType !== selectedContactType ||
      contactValue.trim() !== getContactValue(selectedContact).trim() ||
      contactName.trim() !== getContactName(selectedContact) ||
      makePrimary !== Boolean(selectedContact?.is_primary));

  const mutation = useMutation({
    mutationFn: async (payload: {
      contactType?: TicketContactType;
      value?: string;
      contactName?: string;
      ticketContactId?: number;
      isPrimary?: boolean;
      clear?: boolean;
    }) => {
      if (!ticketId) return;
      return telephonyApi.setTicketContact(ticketId, {
        contact_type: payload.contactType,
        phone: payload.contactType === 'phone' ? payload.value : undefined,
        telegram: payload.contactType === 'telegram' ? payload.value : undefined,
        contact_name: payload.contactName,
        ticket_contact_id: payload.ticketContactId,
        is_primary: payload.isPrimary,
        clear: payload.clear,
      });
    },
    onSuccess: async (_, variables) => {
      if (variables.clear) {
        message.success('Контакт отвязан');
        fillFormFromContact(null);
      } else if (variables.ticketContactId && variables.value !== undefined) {
        message.success('Контакт обновлён');
      } else {
        message.success('Контакт сохранён');
        fillFormFromContact(null);
      }
      await queryClient.invalidateQueries({ queryKey: ['ticket', ticketId] });
      await queryClient.invalidateQueries({ queryKey: ['tickets'] });
      await queryClient.invalidateQueries({ queryKey: ['telephony'] });
      onChanged?.();
    },
    onError: (error) => message.error(extractErrorText(error, 'Не удалось сохранить контакт')),
  });

  const copyValue = async (value: string) => {
    if (!value) {
      message.warning('Контакт не указан');
      return;
    }
    await navigator.clipboard.writeText(value);
    message.success('Контакт скопирован');
  };

  const addContact = () => {
    const value = contactValue.trim();
    if (!value) {
      message.warning(contactType === 'phone' ? 'Укажите телефон' : 'Укажите Telegram');
      return;
    }
    mutation.mutate({
      contactType,
      value,
      contactName: contactName.trim(),
      isPrimary: makePrimary,
    });
  };

  const saveSelectedContact = () => {
    if (!selectedContact?.id) return;
    const value = contactValue.trim();
    if (!value) {
      message.warning(contactType === 'phone' ? 'Укажите телефон' : 'Укажите Telegram');
      return;
    }
    mutation.mutate({
      ticketContactId: selectedContact.id,
      contactType,
      value,
      contactName: contactName.trim(),
      isPrimary: makePrimary,
    });
  };

  const contactLabel = getContactTitle(primaryContact) || 'Контакт не указан';

  return (
    <div style={highlightStyle}>
      <Space size={8} wrap>
        <Button
          type="text"
          disabled={disabled}
          onClick={openContactsModal}
          style={{ height: 'auto', padding: '0 4px', whiteSpace: 'normal', textAlign: 'left' }}
        >
          <Space size={6} wrap>
            {primaryContact?.is_primary && <StarFilled style={{ color: '#faad14' }} />}
            <Text>{contactLabel}</Text>
            {extraCount > 0 && <Tag>+{extraCount}</Tag>}
          </Space>
        </Button>
        <Tooltip title="Скопировать главный контакт">
          <Button
            type="text"
            size="small"
            icon={<CopyOutlined />}
            disabled={!primaryValue}
            onClick={() => void copyValue(primaryValue)}
          />
        </Tooltip>
      </Space>

      <Modal
        open={isModalOpen}
        title="Контакты тикета"
        footer={null}
        onCancel={() => setIsModalOpen(false)}
        destroyOnHidden
      >
        <Space direction="vertical" size={12} style={{ width: '100%' }}>
          {normalizedContacts.length === 0 ? (
            <Text type="secondary">Контакты не указаны</Text>
          ) : (
            normalizedContacts.map((item) => {
              const subtitle = getContactSubtitle(item);
              const isSelected = item.id > 0 && item.id === selectedContactId;
              return (
                <div
                  key={`${item.contact_type}-${item.id || item.value}`}
                  style={{
                    display: 'grid',
                    gridTemplateColumns: '24px minmax(0, 1fr) auto',
                    alignItems: 'center',
                    gap: 8,
                    minHeight: 38,
                  }}
                >
                  <Radio
                    checked={isSelected}
                    disabled={disabled || item.id === 0 || mutation.isPending}
                    onChange={() => fillFormFromContact(item)}
                  />
                  <Space direction="vertical" size={0} style={{ minWidth: 0 }}>
                    <Space size={6} wrap>
                      <Tag color={normalizeContactType(item.contact_type) === 'telegram' ? 'cyan' : 'blue'}>
                        {normalizeContactType(item.contact_type) === 'telegram' ? 'Telegram' : 'Телефон'}
                      </Tag>
                      <Text ellipsis style={{ maxWidth: 300 }}>
                        {getContactTitle(item)}
                      </Text>
                      {item.is_primary && <Tag color="gold">главный</Tag>}
                    </Space>
                    {subtitle && <Text type="secondary">{subtitle}</Text>}
                  </Space>
                  <Space size={4}>
                    <Button
                      type="text"
                      size="small"
                      icon={<CopyOutlined />}
                      onClick={() => void copyValue(getContactValue(item))}
                    />
                    <Button
                      type="text"
                      size="small"
                      danger
                      icon={<DeleteOutlined />}
                      disabled={disabled || item.id === 0}
                      loading={mutation.isPending && mutation.variables?.ticketContactId === item.id && mutation.variables?.clear}
                      onClick={() => mutation.mutate({ ticketContactId: item.id, clear: true })}
                    />
                  </Space>
                </div>
              );
            })
          )}

          {!disabled && (
            <>
              <Divider style={{ margin: '4px 0' }} />
              <Space size={8} wrap>
                <Radio.Group
                  value={contactType}
                  onChange={(event) => setContactType(event.target.value as TicketContactType)}
                >
                  <Radio.Button value="phone">Телефон</Radio.Button>
                  <Radio.Button value="telegram">Telegram</Radio.Button>
                </Radio.Group>
                <Button size="small" icon={<PlusOutlined />} onClick={() => fillFormFromContact(null)}>
                  Новый
                </Button>
              </Space>
              <Input
                value={contactValue}
                onChange={(event) => setContactValue(event.target.value)}
                placeholder={contactType === 'phone' ? 'Номер телефона' : '@telegram_login'}
              />
              <Space.Compact style={{ width: '100%' }}>
                <Input
                  value={contactName}
                  onChange={(event) => setContactName(event.target.value)}
                  placeholder="Имя контакта"
                />
                {hasContactChanges && (
                  <Tooltip title="Сохранить">
                    <Button
                      icon={<SaveOutlined />}
                      loading={mutation.isPending}
                      onClick={saveSelectedContact}
                      aria-label="Сохранить контакт"
                    />
                  </Tooltip>
                )}
              </Space.Compact>
              <Checkbox
                checked={makePrimary}
                disabled={Boolean(selectedContact?.is_primary)}
                onChange={(event) => setMakePrimary(event.target.checked)}
              >
                Сделать главным
              </Checkbox>
              {!isEditingExisting && (
                <Button type="primary" icon={<PlusOutlined />} loading={mutation.isPending} onClick={addContact}>
                  Добавить контакт
                </Button>
              )}
            </>
          )}
        </Space>
      </Modal>
    </div>
  );
};

export default TicketContactsControl;
