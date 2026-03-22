import React, { useEffect, useMemo, useState } from 'react';
import { Alert, Button, Card, Descriptions, Empty, Input, Select, Space, Tag, Typography } from 'antd';
import {
  AgentAdapterManifestDTO,
  AgentCOMSignatureCandidateDTO,
  AgentMachineProfileDTO,
  AgentOperatorFlowDTO,
  SaveAgentMachineProfilePayload,
  UpsertAgentCOMSignatureRulePayload,
} from '@/types/api';
import { formatDateTime } from './agentDiagnosticsUtils';

const { Text, Paragraph } = Typography;
const { TextArea } = Input;

type EditableManifest = AgentAdapterManifestDTO & {
  _key: string;
};

type SignatureDraft = {
  device_type: string;
  label: string;
  confidence: string;
  profile_hint: string;
  suggested_adapter: string;
  notes: string;
};

type AgentOperatorFlowCardProps = {
  operatorFlow?: AgentOperatorFlowDTO | null;
  saveProfilePending: boolean;
  saveProfileError?: string;
  saveSignaturePending: boolean;
  saveSignatureError?: string;
  onSaveProfile: (payload: SaveAgentMachineProfilePayload) => void;
  onSaveSignatureRule: (payload: UpsertAgentCOMSignatureRulePayload) => void;
};

const machineProfileOptions = [
  { value: 'service-workstation', label: 'Сервисная станция' },
  { value: 'pos-workstation', label: 'POS-станция' },
  { value: 'fiscal-workstation', label: 'Фискальная станция' },
  { value: 'hybrid-pos-fiscal', label: 'Гибридная POS/фискальная станция' },
  { value: 'unknown', label: 'Недостаточно данных' },
];

const deviceTypeOptions = [
  { value: 'fiscal', label: 'Фискальное устройство' },
  { value: 'fiscal-atol', label: 'Фискальный регистратор АТОЛ' },
  { value: 'fiscal-mitsu', label: 'Фискальный регистратор Mitsu' },
  { value: 'fiscal-shtrih', label: 'Фискальный регистратор Штрих' },
];

const adapterOptions = [
  { value: 'fiscal-atol', label: 'fiscal-atol' },
  { value: 'fiscal-mitsu', label: 'fiscal-mitsu' },
  { value: 'fiscal-shtrih', label: 'fiscal-shtrih' },
];

const buildManifestKey = (index: number) => `manifest-${index}`;

const toManifestDrafts = (manifests?: AgentAdapterManifestDTO[] | null) => (
  (manifests || []).map((item, index) => ({ ...item, _key: buildManifestKey(index) }))
);

const splitReasons = (value: string) => (
  value
    .split('\n')
    .map((item) => item.trim())
    .filter(Boolean)
);

const toSignatureDraft = (candidate: AgentCOMSignatureCandidateDTO): SignatureDraft => ({
  device_type: candidate.existing_rule?.device_type || candidate.device_type || '',
  label: candidate.existing_rule?.label || candidate.classification_label || candidate.friendly_name || '',
  confidence: candidate.existing_rule?.confidence || 'high',
  profile_hint: candidate.existing_rule?.profile_hint || '',
  suggested_adapter: candidate.existing_rule?.suggested_adapter || candidate.suggested_adapter || '',
  notes: candidate.existing_rule?.notes || '',
});

const AgentOperatorFlowCard: React.FC<AgentOperatorFlowCardProps> = ({
  operatorFlow,
  saveProfilePending,
  saveProfileError,
  saveSignaturePending,
  saveSignatureError,
  onSaveProfile,
  onSaveSignatureRule,
}) => {
  const recommendedProfile = operatorFlow?.recommended_profile || null;
  const savedProfile = operatorFlow?.saved_profile || null;
  const fingerprint = operatorFlow?.meaningful_heartbeat?.fingerprint || '';
  const warnings = operatorFlow?.warnings || [];
  const signatureCandidates = operatorFlow?.signature_candidates || [];

  const [profileDraft, setProfileDraft] = useState<AgentMachineProfileDTO>({
    key: '',
    title: '',
    summary: '',
    source: 'operator',
  });
  const [reasonsText, setReasonsText] = useState('');
  const [manifestDrafts, setManifestDrafts] = useState<EditableManifest[]>([]);
  const [signatureDrafts, setSignatureDrafts] = useState<Record<string, SignatureDraft>>({});

  useEffect(() => {
    const nextProfile = savedProfile || recommendedProfile || { key: '', title: '', summary: '', source: 'operator' };
    setProfileDraft({
      key: nextProfile.key || '',
      title: nextProfile.title || '',
      summary: nextProfile.summary || '',
      source: 'operator',
    });
    setReasonsText((operatorFlow?.saved_reasons || operatorFlow?.recommended_reasons || []).join('\n'));
    setManifestDrafts(toManifestDrafts(
      operatorFlow?.saved_adapter_manifests?.length
        ? operatorFlow.saved_adapter_manifests
        : operatorFlow?.recommended_adapter_manifests,
    ));

    const nextDrafts: Record<string, SignatureDraft> = {};
    signatureCandidates.forEach((candidate) => {
      const signatureKey = (candidate.signature_key || '').trim();
      if (!signatureKey) {
        return;
      }
      nextDrafts[signatureKey] = toSignatureDraft(candidate);
    });
    setSignatureDrafts(nextDrafts);
  }, [operatorFlow, recommendedProfile, savedProfile, signatureCandidates]);

  const hasDraftManifests = manifestDrafts.length > 0;
  const recommendedReasons = operatorFlow?.recommended_reasons || [];
  const effectiveManifests = operatorFlow?.effective_adapter_manifests || [];

  const onManifestChange = (key: string, patch: Partial<EditableManifest>) => {
    setManifestDrafts((current) => current.map((item) => (
      item._key === key ? { ...item, ...patch } : item
    )));
  };

  const addManifest = () => {
    setManifestDrafts((current) => [...current, { _key: `manifest-new-${current.length + 1}` }]);
  };

  const applyRecommendedManifests = () => {
    setManifestDrafts(toManifestDrafts(operatorFlow?.recommended_adapter_manifests));
  };

  const removeManifest = (key: string) => {
    setManifestDrafts((current) => current.filter((item) => item._key !== key));
  };

  const saveProfile = () => {
    onSaveProfile({
      profile: {
        key: (profileDraft.key || '').trim(),
        title: (profileDraft.title || '').trim(),
        summary: (profileDraft.summary || '').trim(),
        source: 'operator',
      },
      reasons: splitReasons(reasonsText),
      adapter_manifests: manifestDrafts
        .map(({ _key, ...item }) => item)
        .filter((item) => (item.adapter_id || '').trim().length > 0),
    });
  };

  const onSignatureDraftChange = (signatureKey: string, patch: Partial<SignatureDraft>) => {
    setSignatureDrafts((current) => ({
      ...current,
      [signatureKey]: {
        ...current[signatureKey],
        ...patch,
      },
    }));
  };

  const saveSignatureRule = (candidate: AgentCOMSignatureCandidateDTO) => {
    const signatureKey = (candidate.signature_key || '').trim();
    if (!signatureKey) {
      return;
    }
    const draft = signatureDrafts[signatureKey];
    onSaveSignatureRule({
      signature_key: signatureKey,
      device_type: draft?.device_type || '',
      label: draft?.label || '',
      confidence: draft?.confidence || 'high',
      profile_hint: draft?.profile_hint || '',
      suggested_adapter: draft?.suggested_adapter || '',
      notes: draft?.notes || '',
    });
  };

  const meaningfulStateAvailable = useMemo(() => Boolean(
    operatorFlow?.meaningful_heartbeat?.last_meaningful_state || fingerprint,
  ), [fingerprint, operatorFlow?.meaningful_heartbeat?.last_meaningful_state]);

  return (
    <Card className="glass-panel" title="Профиль машины и назначение адаптеров" size="small">
      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        {warnings.map((warning, index) => (
          <Alert key={`${warning}-${index}`} type="warning" showIcon message={warning} />
        ))}

        {saveProfileError ? (
          <Alert type="error" showIcon message="Не удалось сохранить профиль машины" description={saveProfileError} />
        ) : null}
        {saveSignatureError ? (
          <Alert type="error" showIcon message="Не удалось сохранить правило COM-сигнатуры" description={saveSignatureError} />
        ) : null}

        <Card
          size="small"
          type="inner"
          title="Meaningful heartbeat"
          extra={fingerprint ? <Tag color="processing">{fingerprint.slice(0, 12)}...</Tag> : <Tag>Нет fingerprint</Tag>}
        >
          {!meaningfulStateAvailable ? (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="Значимое heartbeat-состояние ещё не зафиксировано" />
          ) : (
            <Descriptions bordered size="small" column={2}>
              <Descriptions.Item label="Fingerprint" span={2}>
                {fingerprint ? <Text copyable={{ text: fingerprint }} code>{fingerprint}</Text> : '-'}
              </Descriptions.Item>
              <Descriptions.Item label="Последний значимый heartbeat">
                {formatDateTime(operatorFlow?.meaningful_heartbeat?.last_meaningful_heartbeat_at)}
              </Descriptions.Item>
              <Descriptions.Item label="Последний значимый observed_at">
                {formatDateTime(operatorFlow?.meaningful_heartbeat?.last_meaningful_observed_at)}
              </Descriptions.Item>
            </Descriptions>
          )}
        </Card>

        <Card size="small" type="inner" title="Рекомендация сервера">
          {recommendedProfile ? (
            <Space direction="vertical" size="small" style={{ width: '100%' }}>
              <div>
                <Text strong>{recommendedProfile.title || recommendedProfile.key || 'Профиль не определён'}</Text>
                {recommendedProfile.key ? <div><Text type="secondary" code>{recommendedProfile.key}</Text></div> : null}
              </div>
              {recommendedProfile.summary ? <Paragraph style={{ marginBottom: 0 }}>{recommendedProfile.summary}</Paragraph> : null}
              {recommendedReasons.length > 0 ? (
                <Space direction="vertical" size={4} style={{ width: '100%' }}>
                  <Text strong>Причины рекомендации</Text>
                  {recommendedReasons.map((reason) => (
                    <Text key={reason}>{`• ${reason}`}</Text>
                  ))}
                </Space>
              ) : null}
              <Space direction="vertical" size={4} style={{ width: '100%' }}>
                <Text strong>Рекомендованные adapter_manifests</Text>
                {operatorFlow?.recommended_adapter_manifests?.length ? (
                  operatorFlow.recommended_adapter_manifests.map((item, index) => (
                    <Tag key={`${item.adapter_id || 'manifest'}-${index}`} color="processing" style={{ width: 'fit-content' }}>
                      {item.adapter_id || 'manifest'}
                    </Tag>
                  ))
                ) : (
                  <Text type="secondary">Сервер не смог автоматически подобрать manifest. Профиль можно сохранить вручную.</Text>
                )}
              </Space>
            </Space>
          ) : (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="Рекомендация пока не готова" />
          )}
        </Card>

        <Card
          size="small"
          type="inner"
          title="Подтверждённый профиль и итоговые manifests"
          extra={savedProfile ? <Tag color="success">Сохранено</Tag> : <Tag>Черновик</Tag>}
        >
          <Space direction="vertical" size="middle" style={{ width: '100%' }}>
            <Descriptions bordered size="small" column={2}>
              <Descriptions.Item label="Сохранённый профиль">
                {savedProfile?.title || 'Пока не сохранён'}
              </Descriptions.Item>
              <Descriptions.Item label="Подтвердил">
                {savedProfile?.confirmed_by || '-'}
              </Descriptions.Item>
              <Descriptions.Item label="Сохранено">
                {formatDateTime(savedProfile?.confirmed_at)}
              </Descriptions.Item>
              <Descriptions.Item label="Эффективных manifests">
                {effectiveManifests.length}
              </Descriptions.Item>
            </Descriptions>

            <Space direction="vertical" size={8} style={{ width: '100%' }}>
              <Text strong>Ключ профиля</Text>
              <Select
                value={profileDraft.key}
                options={machineProfileOptions}
                onChange={(value) => setProfileDraft((current) => ({ ...current, key: value }))}
                style={{ width: '100%' }}
              />

              <Text strong>Название профиля</Text>
              <Input
                value={profileDraft.title}
                onChange={(event) => setProfileDraft((current) => ({ ...current, title: event.target.value }))}
              />

              <Text strong>Описание профиля</Text>
              <TextArea
                rows={3}
                value={profileDraft.summary}
                onChange={(event) => setProfileDraft((current) => ({ ...current, summary: event.target.value }))}
              />

              <Text strong>Причины для сохранения</Text>
              <TextArea
                rows={4}
                value={reasonsText}
                onChange={(event) => setReasonsText(event.target.value)}
                placeholder="По одной причине на строку"
              />
            </Space>

            <Space wrap>
              <Button onClick={applyRecommendedManifests}>Заполнить рекомендацией</Button>
              <Button onClick={addManifest}>Добавить manifest</Button>
            </Space>

            <Space direction="vertical" size="middle" style={{ width: '100%' }}>
              {!hasDraftManifests ? (
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="Manifest-черновик пока пуст" />
              ) : manifestDrafts.map((item) => (
                <Card
                  key={item._key}
                  size="small"
                  type="inner"
                  title={item.adapter_id || 'Новый manifest'}
                  extra={<Button danger type="link" onClick={() => removeManifest(item._key)}>Удалить</Button>}
                >
                  <Space direction="vertical" size={8} style={{ width: '100%' }}>
                    <Input
                      placeholder="adapter_id"
                      value={item.adapter_id}
                      onChange={(event) => onManifestChange(item._key, { adapter_id: event.target.value })}
                    />
                    <Input
                      placeholder="adapter_type"
                      value={item.adapter_type}
                      onChange={(event) => onManifestChange(item._key, { adapter_type: event.target.value })}
                    />
                    <Input
                      placeholder="version"
                      value={item.version}
                      onChange={(event) => onManifestChange(item._key, { version: event.target.value })}
                    />
                    <Input
                      placeholder="target_os"
                      value={item.target_os}
                      onChange={(event) => onManifestChange(item._key, { target_os: event.target.value })}
                    />
                    <Input
                      placeholder="target_arch"
                      value={item.target_arch}
                      onChange={(event) => onManifestChange(item._key, { target_arch: event.target.value })}
                    />
                    <Input
                      placeholder="protocol_version"
                      value={item.protocol_version}
                      onChange={(event) => onManifestChange(item._key, { protocol_version: event.target.value })}
                    />
                    <Input
                      placeholder="download_url"
                      value={item.download_url}
                      onChange={(event) => onManifestChange(item._key, { download_url: event.target.value })}
                    />
                    <Input
                      placeholder="sha256"
                      value={item.sha256}
                      onChange={(event) => onManifestChange(item._key, { sha256: event.target.value })}
                    />
                    <Input
                      placeholder="file_name"
                      value={item.file_name}
                      onChange={(event) => onManifestChange(item._key, { file_name: event.target.value })}
                    />
                  </Space>
                </Card>
              ))}
            </Space>

            <Button type="primary" loading={saveProfilePending} onClick={saveProfile}>
              Сохранить профиль и adapter_manifests
            </Button>
          </Space>
        </Card>

        <Card size="small" type="inner" title="Правила COM signature_key">
          {signatureCandidates.length === 0 ? (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="На этой машине нет COM-сигнатур для подтверждения" />
          ) : (
            <Space direction="vertical" size="middle" style={{ width: '100%' }}>
              {signatureCandidates.map((candidate, index) => {
                const signatureKey = (candidate.signature_key || '').trim();
                const draft = signatureDrafts[signatureKey];
                return (
                  <Card
                    key={`${signatureKey || 'candidate'}-${index}`}
                    size="small"
                    type="inner"
                    title={candidate.friendly_name || candidate.port_name || signatureKey || 'COM-устройство'}
                    extra={candidate.existing_rule ? <Tag color="success">Правило есть</Tag> : <Tag>Нужно правило</Tag>}
                  >
                    <Space direction="vertical" size="small" style={{ width: '100%' }}>
                      <Descriptions bordered size="small" column={2}>
                        <Descriptions.Item label="Порт">{candidate.port_name || '-'}</Descriptions.Item>
                        <Descriptions.Item label="Signature key">
                          {signatureKey ? <Text code>{signatureKey}</Text> : '-'}
                        </Descriptions.Item>
                        <Descriptions.Item label="Классификация агента">
                          {candidate.classification_label || candidate.device_type || '-'}
                        </Descriptions.Item>
                        <Descriptions.Item label="Suggested adapter">
                          {candidate.suggested_adapter || candidate.existing_rule?.suggested_adapter || '-'}
                        </Descriptions.Item>
                      </Descriptions>

                      <Select
                        placeholder="device_type"
                        value={draft?.device_type}
                        options={deviceTypeOptions}
                        onChange={(value) => onSignatureDraftChange(signatureKey, { device_type: value })}
                        style={{ width: '100%' }}
                      />
                      <Input
                        placeholder="label"
                        value={draft?.label}
                        onChange={(event) => onSignatureDraftChange(signatureKey, { label: event.target.value })}
                      />
                      <Select
                        placeholder="confidence"
                        value={draft?.confidence}
                        options={[
                          { value: 'high', label: 'high' },
                          { value: 'medium', label: 'medium' },
                          { value: 'low', label: 'low' },
                        ]}
                        onChange={(value) => onSignatureDraftChange(signatureKey, { confidence: value })}
                        style={{ width: '100%' }}
                      />
                      <Input
                        placeholder="profile_hint"
                        value={draft?.profile_hint}
                        onChange={(event) => onSignatureDraftChange(signatureKey, { profile_hint: event.target.value })}
                      />
                      <Select
                        allowClear
                        placeholder="suggested_adapter"
                        value={draft?.suggested_adapter || undefined}
                        options={adapterOptions}
                        onChange={(value) => onSignatureDraftChange(signatureKey, { suggested_adapter: value || '' })}
                        style={{ width: '100%' }}
                      />
                      <TextArea
                        rows={2}
                        placeholder="notes"
                        value={draft?.notes}
                        onChange={(event) => onSignatureDraftChange(signatureKey, { notes: event.target.value })}
                      />
                      <Button type="primary" loading={saveSignaturePending} onClick={() => saveSignatureRule(candidate)}>
                        Сохранить правило signature_key
                      </Button>
                    </Space>
                  </Card>
                );
              })}
            </Space>
          )}
        </Card>
      </Space>
    </Card>
  );
};

export default AgentOperatorFlowCard;
