import React from 'react';
import { Badge, Card, Col, Form, Input, Row, Select, Space, Typography } from 'antd';
import { CompanySearchSelect, CompanySearchOption } from '@/components/companies/CompanySearchSelect';
import { CompanyMode, ContractMode } from '@/types/api';

interface ExistingCompanyDetails {
  title: string;
  additionalName: string;
  address: string;
}

interface ParentCompanyStatus {
  active_contract: boolean;
  contract_type: string;
}

interface AcceptanceFormProps {
  form: any;
  companyMode: CompanyMode;
  selectedContractMode: ContractMode;
  selectedParentCompany?: ParentCompanyStatus;
  selectedExistingCompany?: ExistingCompanyDetails;
  companyOptions: CompanySearchOption[];
  isCompaniesLoading: boolean;
  isBitrixEnabled: boolean;
  bitrixServicePointOptions: Array<{ value: number; label: string }>;
  isBitrixServicePointsLoading: boolean;
  onCompanySearch?: (value: string) => void;
}

const { Text } = Typography;

const compact: React.CSSProperties = { marginBottom: 10 };

export const AcceptanceForm: React.FC<AcceptanceFormProps> = ({
  form,
  companyMode,
  selectedContractMode,
  selectedParentCompany,
  selectedExistingCompany,
  companyOptions,
  isCompaniesLoading,
  isBitrixEnabled,
  bitrixServicePointOptions,
  isBitrixServicePointsLoading,
  onCompanySearch = () => {},
}) => {
  return (
    <Card size="small" title="Компания и контракт">
      <Form form={form} layout="vertical">
        <Row gutter={12}>
          <Col span={12}>
            <Form.Item name="company_mode" label="Компания" style={compact}>
              <Select>
                <Select.Option value="existing">Выбрать существующую</Select.Option>
                <Select.Option value="new">Создать новую</Select.Option>
              </Select>
            </Form.Item>

            {companyMode === 'existing' ? (
              <Space direction="vertical" size={2} style={{ width: '100%' }}>
                <Form.Item
                  name="company_id"
                  label="Компания"
                  style={compact}
                  rules={[{ required: true, message: 'Выберите компанию' }]}
                >
                  <CompanySearchSelect
                    placeholder="Начните ввод названия компании"
                    options={companyOptions}
                    loading={isCompaniesLoading}
                    onSearch={onCompanySearch}
                  />
                </Form.Item>
                <Form.Item label="Название компании" style={compact}>
                  <Input value={selectedExistingCompany?.title || ''} disabled />
                </Form.Item>
                <Form.Item label="Юридическое название" style={compact}>
                  <Input value={selectedExistingCompany?.additionalName || ''} disabled />
                </Form.Item>
                <Form.Item label="Адрес" style={{ marginBottom: 0 }}>
                  <Input value={selectedExistingCompany?.address || ''} disabled />
                </Form.Item>
              </Space>
            ) : (
              <Space direction="vertical" size={2} style={{ width: '100%' }}>
                <Form.Item
                  name="new_company_title"
                  label="Название компании"
                  style={compact}
                  rules={[{ required: true, message: 'Введите название компании' }]}
                >
                  <Input />
                </Form.Item>
                <Form.Item name="new_company_additional_name" label="Юридическое название" style={compact}>
                  <Input />
                </Form.Item>
                <Form.Item name="new_company_address" label="Адрес" style={{ marginBottom: 0 }}>
                  <Input />
                </Form.Item>
              </Space>
            )}
          </Col>

          <Col span={12}>
            {companyMode === 'new' && (
              <Space direction="vertical" size={2} style={{ width: '100%' }}>
                <Form.Item name="new_company_parent_id" label="Родительская компания" style={compact}>
                  <CompanySearchSelect
                    allowClear
                    options={companyOptions}
                    loading={isCompaniesLoading}
                    placeholder="Выберите родительскую компанию"
                    onSearch={onCompanySearch}
                  />
                </Form.Item>
                <Form.Item
                  name="contract_mode"
                  label="Сценарий контракта"
                  style={compact}
                  rules={[{ required: true, message: 'Выберите сценарий контракта' }]}
                >
                  <Select>
                    <Select.Option value="inherit_parent">Наследовать контракт родителя</Select.Option>
                    <Select.Option value="new">Создать новый контракт</Select.Option>
                  </Select>
                </Form.Item>
                {selectedContractMode !== 'new' && (
                  <Form.Item label="Статус контракта родителя" style={compact}>
                    {selectedParentCompany ? (
                      <Badge
                        status={selectedParentCompany.active_contract ? 'success' : 'error'}
                        text={
                          selectedParentCompany.active_contract
                            ? `Активен: ${selectedParentCompany.contract_type || 'Тип не указан'}`
                            : 'Неактивен'
                        }
                      />
                    ) : (
                      <Text type="secondary">Выберите родительскую компанию</Text>
                    )}
                  </Form.Item>
                )}
                {selectedContractMode === 'new' && (
                  <Form.Item
                    name="contract_type"
                    label="Тип обслуживания"
                    style={compact}
                    rules={[{ required: true, message: 'Выберите тип обслуживания' }]}
                  >
                    <Select>
                      <Select.Option value="TS Cloud">TS Cloud</Select.Option>
                      <Select.Option value="TS Standart (без выездов)">TS Standart (без выездов)</Select.Option>
                      <Select.Option value="TS Standart">TS Standart</Select.Option>
                    </Select>
                  </Form.Item>
                )}
              </Space>
            )}

            {isBitrixEnabled && (
              <Form.Item
                name="bitrix_service_point_id"
                label="Точка обслуживания Bitrix24"
                style={{ marginBottom: 0 }}
                rules={[{ required: true, message: 'Выберите точку обслуживания Bitrix24' }]}
              >
                <Select
                  showSearch
                  placeholder="Выберите точку обслуживания"
                  optionFilterProp="label"
                  loading={isBitrixServicePointsLoading}
                  options={bitrixServicePointOptions}
                />
              </Form.Item>
            )}
          </Col>
        </Row>
      </Form>
    </Card>
  );
};
