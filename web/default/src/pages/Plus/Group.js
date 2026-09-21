import React, { useEffect, useState } from 'react';
import {
  Container,
  Header,
  Table,
  Loader,
  Button,
  Input,
  Dropdown,
  Label,
  Segment,
} from 'semantic-ui-react';
import PlusAPI from '../../helpers/plus';
import { showError, showSuccess } from '../../helpers/utils';

const STRATEGY = [
  { key: 'first_available', text: '优先可用', value: 'first_available' },
  { key: 'random', text: '随机', value: 'random' },
  { key: 'round_robin', text: '轮询', value: 'round_robin' },
  { key: 'capability', text: '按能力', value: 'capability' },
];

export default function Group() {
  const [groups, setGroups] = useState([]);
  const [loading, setLoading] = useState(false);
  const [name, setName] = useState('');
  const [strategy, setStrategy] = useState('first_available');
  const [member, setMember] = useState({ group_name: '', model: '', weight: 1 });

  const load = async () => {
    setLoading(true);
    try {
      const env = await PlusAPI.getModelGroups();
      if (!env.success) return showError(env.message);
      setGroups(env.data || []);
    } catch (e) {
      showError(e.message);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const addGroup = async () => {
    if (!name) return showError('组名不能为空');
    try {
      const env = await PlusAPI.addModelGroup({ group_name: name, strategy });
      if (!env.success) return showError(env.message);
      showSuccess('已创建模型组');
      setName('');
      load();
    } catch (e) {
      showError(e.message);
    }
  };

  const removeGroup = async (id) => {
    try {
      const env = await PlusAPI.removeModelGroup(id);
      if (!env.success) return showError(env.message);
      load();
    } catch (e) {
      showError(e.message);
    }
  };

  const addMember = async () => {
    if (!member.group_name || !member.model) return showError('请选择组并填写模型名');
    try {
      const env = await PlusAPI.addGroupMember({
        group_name: member.group_name,
        model_name: member.model,
        weight: Number(member.weight) || 1,
      });
      if (!env.success) return showError(env.message);
      showSuccess('已添加成员');
      setMember({ group_name: '', model: '', weight: 1 });
      load();
    } catch (e) {
      showError(e.message);
    }
  };

  return (
    <Container style={{ marginTop: '2em', marginBottom: '4em' }}>
      <Header as='h2'>模型组</Header>
      <p style={{ color: '#666' }}>用一个逻辑名聚合多个真实模型，按策略（优先可用 / 随机 / 轮询 / 按能力）自动选路。</p>

      <Segment>
        <Header as='h4'>新建模型组</Header>
        <div style={{ display: 'flex', gap: '0.5em', flexWrap: 'wrap', alignItems: 'center' }}>
          <Input placeholder='组名（如 gpt-latest）' value={name} onChange={(e, d) => setName(d.value)} style={{ width: '220px' }} />
          <Dropdown selection options={STRATEGY} value={strategy} onChange={(e, d) => setStrategy(d.value)} />
          <Button primary onClick={addGroup}>
            创建
          </Button>
        </div>
        <Header as='h4'>添加成员</Header>
        <div style={{ display: 'flex', gap: '0.5em', flexWrap: 'wrap', alignItems: 'center' }}>
          <Dropdown
            selection
            search
            allowAdditions
            placeholder='选择或输入组名'
            value={member.group_name}
            options={groups.map((g) => ({ key: g.group_name, text: g.group_name, value: g.group_name }))}
            onChange={(e, d) => setMember({ ...member, group_name: d.value })}
            style={{ width: '200px' }}
          />
          <Input placeholder='真实模型名' value={member.model} onChange={(e, d) => setMember({ ...member, model: d.value })} style={{ width: '220px' }} />
          <Input placeholder='权重' value={member.weight} onChange={(e, d) => setMember({ ...member, weight: d.value })} style={{ width: '100px' }} />
          <Button onClick={addMember}>添加</Button>
        </div>
      </Segment>

      {loading ? (
        <Loader active inline='centered'>
          加载中
        </Loader>
      ) : (
        <Table celled striped>
          <Table.Header>
            <Table.Row>
              <Table.HeaderCell>ID</Table.HeaderCell>
              <Table.HeaderCell>组名</Table.HeaderCell>
              <Table.HeaderCell>策略</Table.HeaderCell>
              <Table.HeaderCell>启用</Table.HeaderCell>
              <Table.HeaderCell>成员</Table.HeaderCell>
              <Table.HeaderCell>操作</Table.HeaderCell>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {groups.map((g) => (
              <Table.Row key={g.id}>
                <Table.Cell>{g.id}</Table.Cell>
                <Table.Cell>{g.group_name}</Table.Cell>
                <Table.Cell>{g.strategy}</Table.Cell>
                <Table.Cell>{g.enabled ? <Label color='green' size='small'>是</Label> : <Label size='small'>否</Label>}</Table.Cell>
                <Table.Cell>
                  {(g.members || []).map((m, i) => (
                    <Label key={i} style={{ margin: '2px' }}>
                      {m.model_name} ×{m.weight}
                    </Label>
                  ))}
                  {(!g.members || g.members.length === 0) && '-'}
                </Table.Cell>
                <Table.Cell>
                  <Button size='mini' negative onClick={() => removeGroup(g.id)}>
                    删除
                  </Button>
                </Table.Cell>
              </Table.Row>
            ))}
            {groups.length === 0 && (
              <Table.Row>
                <Table.Cell colSpan='6' textAlign='center'>
                  暂无模型组
                </Table.Cell>
              </Table.Row>
            )}
          </Table.Body>
        </Table>
      )}
    </Container>
  );
}
