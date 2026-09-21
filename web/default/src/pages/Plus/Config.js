import React, { useEffect, useState } from 'react';
import {
  Container,
  Header,
  Table,
  Loader,
  Button,
  Input,
  Label,
  Segment,
} from 'semantic-ui-react';
import PlusAPI from '../../helpers/plus';
import { showError, showSuccess } from '../../helpers/utils';

export default function Config() {
  const [aliases, setAliases] = useState([]);
  const [prices, setPrices] = useState([]);
  const [budget, setBudget] = useState(null);
  const [loading, setLoading] = useState(false);
  const [aliasForm, setAliasForm] = useState({ alias: '', target: '' });
  const [priceForm, setPriceForm] = useState({ model_name: '', input_price: '', output_price: '' });
  const [budgetForm, setBudgetForm] = useState({ user_id: '', daily: '', monthly: '' });

  const loadAll = async () => {
    setLoading(true);
    try {
      const [a, p, b] = await Promise.all([
        PlusAPI.getAliasList().catch(() => null),
        PlusAPI.getPriceList().catch(() => null),
        PlusAPI.getBudget().catch(() => null),
      ]);
      if (a && a.success) setAliases(a.data || []);
      if (p && p.success) setPrices(p.data || []);
      if (b && b.success) {
        setBudget(b.data.budget);
        setBudgetForm({
          user_id: b.data.budget?.user_id || '',
          daily: b.data.budget?.daily_quota || '',
          monthly: b.data.budget?.monthly_quota || '',
        });
      }
    } catch (e) {
      showError(e.message);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadAll();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const addAlias = async () => {
    if (!aliasForm.alias || !aliasForm.target) return showError('别名与目标模型不能为空');
    try {
      const env = await PlusAPI.addAlias(aliasForm);
      if (!env.success) return showError(env.message);
      showSuccess('已添加别名');
      setAliasForm({ alias: '', target: '' });
      loadAll();
    } catch (e) {
      showError(e.message);
    }
  };

  const delAlias = async (id) => {
    try {
      const env = await PlusAPI.deleteAlias(id);
      if (!env.success) return showError(env.message);
      loadAll();
    } catch (e) {
      showError(e.message);
    }
  };

  const addPrice = async () => {
    if (!priceForm.model_name) return showError('模型名不能为空');
    try {
      const env = await PlusAPI.upsertPrice({
        model_name: priceForm.model_name,
        input_price: Number(priceForm.input_price) || 0,
        output_price: Number(priceForm.output_price) || 0,
      });
      if (!env.success) return showError(env.message);
      showSuccess('已保存定价');
      setPriceForm({ model_name: '', input_price: '', output_price: '' });
      loadAll();
    } catch (e) {
      showError(e.message);
    }
  };

  const delPrice = async (id) => {
    try {
      const env = await PlusAPI.deletePrice(id);
      if (!env.success) return showError(env.message);
      loadAll();
    } catch (e) {
      showError(e.message);
    }
  };

  const saveBudget = async () => {
    try {
      const env = await PlusAPI.updateBudget({
        user_id: Number(budgetForm.user_id),
        daily_quota: Number(budgetForm.daily) || 0,
        monthly_quota: Number(budgetForm.monthly) || 0,
      });
      if (!env.success) return showError(env.message);
      showSuccess('已更新预算');
      loadAll();
    } catch (e) {
      showError(e.message);
    }
  };

  if (loading) return <Loader active inline='centered'>加载中</Loader>;

  return (
    <Container style={{ marginTop: '2em', marginBottom: '4em' }}>
      <Header as='h2'>别名 / 定价 / 预算</Header>

      <Segment>
        <Header as='h4'>模型别名</Header>
        <div style={{ display: 'flex', gap: '0.5em', flexWrap: 'wrap', alignItems: 'center', marginBottom: '0.5em' }}>
          <Input placeholder='别名（如 gpt-4）' value={aliasForm.alias} onChange={(e, d) => setAliasForm({ ...aliasForm, alias: d.value })} style={{ width: '200px' }} />
          <Input placeholder='目标模型' value={aliasForm.target} onChange={(e, d) => setAliasForm({ ...aliasForm, target: d.value })} style={{ width: '220px' }} />
          <Button primary onClick={addAlias}>添加</Button>
        </div>
        <Table celled striped size='small'>
          <Table.Header>
            <Table.Row>
              <Table.HeaderCell>ID</Table.HeaderCell>
              <Table.HeaderCell>别名</Table.HeaderCell>
              <Table.HeaderCell>目标</Table.HeaderCell>
              <Table.HeaderCell>启用</Table.HeaderCell>
              <Table.HeaderCell>操作</Table.HeaderCell>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {aliases.map((a) => (
              <Table.Row key={a.id}>
                <Table.Cell>{a.id}</Table.Cell>
                <Table.Cell>{a.alias}</Table.Cell>
                <Table.Cell>{a.target}</Table.Cell>
                <Table.Cell>{a.enabled ? <Label color='green' size='small'>是</Label> : <Label size='small'>否</Label>}</Table.Cell>
                <Table.Cell>
                  <Button size='mini' negative onClick={() => delAlias(a.id)}>删除</Button>
                </Table.Cell>
              </Table.Row>
            ))}
            {aliases.length === 0 && (
              <Table.Row>
                <Table.Cell colSpan='5' textAlign='center'>暂无别名</Table.Cell>
              </Table.Row>
            )}
          </Table.Body>
        </Table>
      </Segment>

      <Segment>
        <Header as='h4'>模型定价（美元 / 1M tokens）</Header>
        <div style={{ display: 'flex', gap: '0.5em', flexWrap: 'wrap', alignItems: 'center', marginBottom: '0.5em' }}>
          <Input placeholder='模型名' value={priceForm.model_name} onChange={(e, d) => setPriceForm({ ...priceForm, model_name: d.value })} style={{ width: '220px' }} />
          <Input placeholder='输入价' value={priceForm.input_price} onChange={(e, d) => setPriceForm({ ...priceForm, input_price: d.value })} style={{ width: '120px' }} />
          <Input placeholder='输出价' value={priceForm.output_price} onChange={(e, d) => setPriceForm({ ...priceForm, output_price: d.value })} style={{ width: '120px' }} />
          <Button primary onClick={addPrice}>保存</Button>
        </div>
        <Table celled striped size='small'>
          <Table.Header>
            <Table.Row>
              <Table.HeaderCell>ID</Table.HeaderCell>
              <Table.HeaderCell>模型</Table.HeaderCell>
              <Table.HeaderCell>输入价</Table.HeaderCell>
              <Table.HeaderCell>输出价</Table.HeaderCell>
              <Table.HeaderCell>操作</Table.HeaderCell>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {prices.map((p) => (
              <Table.Row key={p.id}>
                <Table.Cell>{p.id}</Table.Cell>
                <Table.Cell>{p.model_name}</Table.Cell>
                <Table.Cell>{p.input_price}</Table.Cell>
                <Table.Cell>{p.output_price}</Table.Cell>
                <Table.Cell>
                  <Button size='mini' negative onClick={() => delPrice(p.id)}>删除</Button>
                </Table.Cell>
              </Table.Row>
            ))}
            {prices.length === 0 && (
              <Table.Row>
                <Table.Cell colSpan='5' textAlign='center'>暂无定价</Table.Cell>
              </Table.Row>
            )}
          </Table.Body>
        </Table>
      </Segment>

      <Segment>
        <Header as='h4'>用户预算（配额单位）</Header>
        <div style={{ display: 'flex', gap: '0.5em', flexWrap: 'wrap', alignItems: 'center' }}>
          <Input placeholder='用户ID' value={budgetForm.user_id} onChange={(e, d) => setBudgetForm({ ...budgetForm, user_id: d.value })} style={{ width: '120px' }} />
          <Input placeholder='日额度' value={budgetForm.daily} onChange={(e, d) => setBudgetForm({ ...budgetForm, daily: d.value })} style={{ width: '140px' }} />
          <Input placeholder='月额度' value={budgetForm.monthly} onChange={(e, d) => setBudgetForm({ ...budgetForm, monthly: d.value })} style={{ width: '140px' }} />
          <Button primary onClick={saveBudget}>保存</Button>
        </div>
      </Segment>
    </Container>
  );
}
