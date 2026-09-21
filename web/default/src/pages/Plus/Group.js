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
  Message,
  Checkbox,
  Icon,
} from 'semantic-ui-react';
import PlusAPI from '../../helpers/plus';
import { showError, showSuccess } from '../../helpers/utils';

const STRATEGY = [
  { key: 'first_available', text: '优先可用', value: 'first_available' },
  { key: 'random', text: '随机', value: 'random' },
  { key: 'round_robin', text: '轮询', value: 'round_robin' },
  { key: 'capability', text: '按能力', value: 'capability' },
];

// 复制到剪贴板，方便用户把逻辑组名填进令牌
const copy = (text) => {
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(text).then(
      () => showSuccess(`已复制 ${text}`),
      () => showError('复制失败，请手动选中复制'),
    );
  } else {
    showError('当前浏览器不支持自动复制，请手动选中复制');
  }
};

export default function Group() {
  const [groups, setGroups] = useState([]);
  const [loading, setLoading] = useState(false);
  const [autoing, setAutoing] = useState(false);
  const [dryRun, setDryRun] = useState(false);
  const [lastAuto, setLastAuto] = useState(null);
  const [availModels, setAvailModels] = useState([]);
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

  // 拉一次当前可用模型，用于「添加成员」时下拉选择，避免手打模型名
  const loadAvailModels = async () => {
    try {
      const env = await PlusAPI.getUserAvailableModels();
      if (env.success) setAvailModels(env.data || []);
    } catch (e) {
      /* 忽略：可用模型列不出来不影响手工填 */
    }
  };

  useEffect(() => {
    load();
    loadAvailModels();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 一键自动建组：把当前分组下的可用模型聚合成 auto-chat / auto-coding / ...
  const autoBuild = async () => {
    setAutoing(true);
    try {
      const env = await PlusAPI.autoModelGroups({ dry_run: dryRun });
      if (!env.success) return showError(env.message);
      setLastAuto(env.data || null);
      showSuccess(env.message || '已自动建组');
      if (!dryRun) load();
    } catch (e) {
      showError(e.message);
    } finally {
      setAutoing(false);
    }
  };

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

  const groupOptions = groups.map((g) => ({
    key: g.group_name,
    text: g.group_name,
    value: g.group_name,
  }));

  return (
    <Container style={{ marginTop: '2em', marginBottom: '4em' }}>
      <Header as='h2'>模型组</Header>
      <p style={{ color: '#666' }}>
        用一个逻辑名聚合多个真实模型，按策略（优先可用 / 随机 / 轮询 / 按能力）自动选路。
        在令牌里只勾这个逻辑名，之后上游换型号、加渠道都不用再改客户端配置。
      </p>

      <Segment color='blue'>
        <Header as='h4'>
          <Icon name='magic' />
          一键自动建组
        </Header>
        <p style={{ color: '#666', marginBottom: '0.8em' }}>
          按当前分组下「真正有可用渠道」的模型自动归类，生成
          <Label size='tiny' style={{ margin: '0 4px' }}>
            auto-chat
          </Label>
          <Label size='tiny' style={{ margin: '0 4px' }}>
            auto-coding
          </Label>
          <Label size='tiny' style={{ margin: '0 4px' }}>
            auto-vision
          </Label>
          <Label size='tiny' style={{ margin: '0 4px' }}>
            auto-reasoning
          </Label>
          <Label size='tiny' style={{ margin: '0 4px' }}>
            auto-cheap
          </Label>
          <Label size='tiny' style={{ margin: '0 4px' }}>
            auto-all
          </Label>
          等逻辑组。上游换型号后重跑一次即可自愈。
        </p>
        <div style={{ display: 'flex', gap: '0.8em', alignItems: 'center', flexWrap: 'wrap' }}>
          <Button primary loading={autoing} disabled={autoing} onClick={autoBuild}>
            {dryRun ? '预演一下' : '自动建组'}
          </Button>
          <Checkbox
            toggle
            label='只预演不写库'
            checked={dryRun}
            onChange={(e, d) => setDryRun(!!d.checked)}
          />
        </div>

        {lastAuto && (
          <Message
            style={{ marginTop: '1em' }}
            positive={!dryRun}
            warning={dryRun}
            icon='info'
          >
            <Message.Header>
              {dryRun ? '预演结果（未写入数据库）' : '自动建组结果'}
            </Message.Header>
            <p>
              参与分组的可用模型：<strong>{lastAuto.models}</strong> 个。
              新建 <strong>{(lastAuto.created || []).length}</strong> 组，
              刷新 <strong>{(lastAuto.updated || []).length}</strong> 组。
            </p>
            {(lastAuto.group_names || []).length > 0 && (
              <>
                <p style={{ marginBottom: '0.4em' }}>
                  把这些逻辑名填进令牌的「模型」里即可（点一下复制）：
                </p>
                <div>
                  {(lastAuto.group_names || []).map((n) => (
                    <Label
                      key={n}
                      as='a'
                      basic
                      color='blue'
                      style={{ margin: '3px', cursor: 'pointer' }}
                      onClick={() => copy(n)}
                    >
                      {n}
                      <Icon name='copy' style={{ marginLeft: '5px' }} />
                    </Label>
                  ))}
                </div>
              </>
            )}
            {(lastAuto.skipped || []).length > 0 && (
              <p style={{ color: '#888', marginTop: '0.5em' }}>
                跳过（当前没有匹配的可用模型）：{(lastAuto.skipped || []).join('、')}
              </p>
            )}
          </Message>
        )}
      </Segment>

      <Segment>
        <Header as='h4'>手工建组 / 加成员</Header>
        <div style={{ display: 'flex', gap: '0.5em', flexWrap: 'wrap', alignItems: 'center' }}>
          <Input
            placeholder='组名（如 gpt-latest）'
            value={name}
            onChange={(e, d) => setName(d.value)}
            style={{ width: '220px' }}
          />
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
            options={groupOptions}
            onChange={(e, d) => setMember({ ...member, group_name: d.value })}
            style={{ width: '200px' }}
          />
          <Dropdown
            selection
            search
            allowAdditions
            placeholder='真实模型名'
            value={member.model}
            options={availModels.map((m) => ({ key: m, text: m, value: m }))}
            onChange={(e, d) => setMember({ ...member, model: d.value })}
            style={{ width: '240px' }}
          />
          <Input
            placeholder='权重'
            value={member.weight}
            onChange={(e, d) => setMember({ ...member, weight: d.value })}
            style={{ width: '100px' }}
          />
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
            {groups.map((g) => {
              const isAuto = (g.group_name || '').startsWith('auto-');
              return (
                <Table.Row key={g.id}>
                  <Table.Cell>{g.id}</Table.Cell>
                  <Table.Cell>
                    {g.group_name}{' '}
                    {isAuto && (
                      <Label size='tiny' color='blue'>
                        自动
                      </Label>
                    )}
                    <Icon
                      name='copy'
                      link
                      style={{ marginLeft: '6px' }}
                      onClick={() => copy(g.group_name)}
                    />
                  </Table.Cell>
                  <Table.Cell>{g.strategy}</Table.Cell>
                  <Table.Cell>
                    {g.enabled ? (
                      <Label color='green' size='small'>
                        是
                      </Label>
                    ) : (
                      <Label size='small'>否</Label>
                    )}
                  </Table.Cell>
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
              );
            })}
            {groups.length === 0 && (
              <Table.Row>
                <Table.Cell colSpan='6' textAlign='center'>
                  暂无模型组 —— 点上面的「自动建组」，一步就能生成
                </Table.Cell>
              </Table.Row>
            )}
          </Table.Body>
        </Table>
      )}
    </Container>
  );
}
