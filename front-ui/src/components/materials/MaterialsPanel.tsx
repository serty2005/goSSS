import React, { useEffect, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Button, Card, Empty, List, Popconfirm, Space, Typography } from 'antd';
import { PaperClipOutlined } from '@ant-design/icons';
import { useSearchParams } from 'react-router-dom';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { materialsApi } from '@/api/materials';
import { MaterialDTO } from '@/types/api';
import LoadingPlaceholder from '@/components/common/LoadingPlaceholder';
import MaterialEditorModal from '@/components/materials/MaterialEditorModal';
import MaterialFilesList from '@/components/materials/MaterialFilesList';
import { useDeleteMaterial, useDeleteMaterialFile } from '@/components/materials/useMaterialMutations';

const { Text } = Typography;

type MaterialEntityType = 'Company' | 'Server' | 'Workstation' | 'FiscalRegister';

interface MaterialsPanelProps {
  entityType: MaterialEntityType;
  entityID: string;
  title?: string;
}

const MaterialsPanel: React.FC<MaterialsPanelProps> = ({ entityType, entityID, title }) => {
  const [selectedID, setSelectedID] = useState<string>('');
  const [searchParams, setSearchParams] = useSearchParams();
  const requestedMaterialID = searchParams.get('material') || '';
  const requestedEdit = searchParams.get('edit') === '1';
  const [pendingEditID, setPendingEditID] = useState('');
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [editingMaterialID, setEditingMaterialID] = useState<string | null>(null);

  const { data: materialsRes, isLoading } = useQuery({
    queryKey: ['materials', entityType, entityID],
    queryFn: () => materialsApi.listByEntity(entityType, entityID, 100, 0),
    enabled: Boolean(entityID),
    staleTime: 30_000,
  });

  const materials = useMemo(() => materialsRes?.data || [], [materialsRes?.data]);
  // Редактируемый материал берется из списка, чтобы форма видела актуальные файлы.
  const editingMaterial = useMemo(
    () => (editingMaterialID ? materials.find((item) => item.id === editingMaterialID) || null : null),
    [editingMaterialID, materials],
  );
  const { deleteFileMutation, deletingFileID } = useDeleteMaterialFile();
  const deleteMutation = useDeleteMaterial();

  useEffect(() => {
    if (!requestedMaterialID || !materials.some((item) => item.id === requestedMaterialID)) {
      return;
    }
    setSelectedID(requestedMaterialID);
    if (requestedEdit) {
      setPendingEditID(requestedMaterialID);
    }
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      next.delete('material');
      next.delete('edit');
      return next;
    }, { replace: true });
  }, [materials, requestedEdit, requestedMaterialID, setSearchParams]);

  useEffect(() => {
    if (!materials.length) {
      setSelectedID('');
      return;
    }
    if (requestedMaterialID && materials.some((item) => item.id === requestedMaterialID)) {
      return;
    }
    if (!selectedID || !materials.some((item) => item.id === selectedID)) {
      setSelectedID(materials[0].id);
    }
  }, [materials, requestedMaterialID, selectedID]);

  const selectedMaterial = useMemo(
    () => materials.find((item) => item.id === selectedID) || null,
    [materials, selectedID],
  );

  const openCreate = () => {
    setEditingMaterialID(null);
    setIsModalOpen(true);
  };

  const openEdit = (item: MaterialDTO) => {
    setEditingMaterialID(item.id);
    setIsModalOpen(true);
  };

  useEffect(() => {
    if (!pendingEditID || selectedMaterial?.id !== pendingEditID) {
      return;
    }
    // Запрос редактирования пришел из URL (?material=...&edit=1).
    setPendingEditID('');
    setEditingMaterialID(selectedMaterial.id);
    setIsModalOpen(true);
  }, [pendingEditID, selectedMaterial]);

  return (
    <div className="materials-panel">
      <Card
        size="small"
        title={title || 'Материалы'}
        extra={<Button size="small" type="primary" onClick={openCreate}>Новый</Button>}
      >
        {isLoading ? (
          <LoadingPlaceholder />
        ) : materials.length === 0 ? (
          <Empty description="Материалов пока нет" />
        ) : (
          <List
            size="small"
            dataSource={materials}
            renderItem={(item) => (
              <List.Item
                style={{
                  cursor: 'pointer',
                  background: selectedID === item.id ? 'rgba(24, 144, 255, 0.08)' : undefined,
                  borderRadius: 8,
                  paddingInline: 8,
                }}
                onClick={() => setSelectedID(item.id)}
              >
                <Space direction="vertical" size={0} style={{ width: '100%' }}>
                  <Text strong>
                    {item.files.length > 0 ? <PaperClipOutlined style={{ marginInlineEnd: 6 }} /> : null}
                    {item.subject}
                  </Text>
                  <Text type="secondary" style={{ fontSize: 12 }}>
                    {item.author_name || 'Сотрудник'} • {new Date(item.updated_at).toLocaleString()}
                    {item.files.length > 0 ? ` • файлов: ${item.files.length}` : ''}
                  </Text>
                </Space>
              </List.Item>
            )}
          />
        )}
      </Card>

      <Card
        size="small"
        title={selectedMaterial?.subject || 'Просмотр материала'}
        extra={selectedMaterial ? (
          <Space>
            <Button size="small" onClick={() => openEdit(selectedMaterial)}>Редактировать</Button>
            <Popconfirm
              title="Удалить материал?"
              okText="Удалить"
              cancelText="Отмена"
              onConfirm={() => deleteMutation.mutate(selectedMaterial.id)}
            >
              <Button size="small" danger loading={deleteMutation.isPending}>Удалить</Button>
            </Popconfirm>
          </Space>
        ) : null}
      >
        {!selectedMaterial ? (
          <Empty description="Выберите материал из списка" />
        ) : (
          <Space direction="vertical" size={12} style={{ width: '100%' }}>
            <Text type="secondary">
              Автор: {selectedMaterial.author_name || 'Сотрудник'} • Создан: {new Date(selectedMaterial.created_at).toLocaleString()} • Обновлён: {new Date(selectedMaterial.updated_at).toLocaleString()}
            </Text>
            {selectedMaterial.files.length > 0 ? (
              <MaterialFilesList
                files={selectedMaterial.files}
                onDelete={(file) => deleteFileMutation.mutate({ materialID: selectedMaterial.id, fileID: file.id })}
                deletingFileID={deletingFileID}
              />
            ) : null}
            {selectedMaterial.content ? (
              <div className="markdown-body" style={{ whiteSpace: 'normal' }}>
                <ReactMarkdown remarkPlugins={[remarkGfm]}>
                  {selectedMaterial.content}
                </ReactMarkdown>
              </div>
            ) : null}
          </Space>
        )}
      </Card>

      <MaterialEditorModal
        open={isModalOpen}
        material={editingMaterial}
        defaultRefs={[{ entity_type: entityType, entity_id: entityID }]}
        pinnedRef={{ entity_type: entityType, entity_id: entityID }}
        onClose={() => {
          setIsModalOpen(false);
          setEditingMaterialID(null);
        }}
      />
    </div>
  );
};

export default MaterialsPanel;
