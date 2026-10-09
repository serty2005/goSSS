import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { message } from 'antd';
import { materialsApi } from '@/api/materials';
import { withApiError } from '@/utils/apiError';

// useDeleteMaterialFile удаляет один файл материала и обновляет все списки материалов.
export const useDeleteMaterialFile = () => {
  const queryClient = useQueryClient();
  const [deletingFileID, setDeletingFileID] = useState('');
  const deleteFileMutation = useMutation({
    mutationFn: ({ materialID, fileID }: { materialID: string; fileID: string }) => materialsApi.deleteFile(materialID, fileID),
    onMutate: ({ fileID }) => setDeletingFileID(fileID),
    onSettled: () => setDeletingFileID(''),
    onSuccess: () => {
      message.success('Файл удалён');
      queryClient.invalidateQueries({ queryKey: ['materials'] });
    },
    onError: (error) => {
      message.error(withApiError('Не удалось удалить файл', error));
    },
  });
  return { deleteFileMutation, deletingFileID };
};

// useDeleteMaterial удаляет материал вместе с его файлами и обновляет все списки материалов.
export const useDeleteMaterial = () => {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => materialsApi.delete(id),
    onSuccess: () => {
      message.success('Материал удалён');
      queryClient.invalidateQueries({ queryKey: ['materials'] });
    },
    onError: (error) => {
      message.error(withApiError('Не удалось удалить материал', error));
    },
  });
};
