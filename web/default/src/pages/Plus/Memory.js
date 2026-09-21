import React, { useEffect, useState } from 'react';
import {
  Container,
  Header,
  Table,
  Loader,
  Button,
  Input,
  Label,
  TextArea,
  Segment,
  Message,
} from 'semantic-ui-react';
import PlusAPI from '../../helpers/plus';
import { showError, showSuccess } from '../../helpers/utils';

export default function Memory() {
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(false);
  const [form, setForm] = useState({ session_id: '', scope: 'long', role: 'user', content: '' });
  const [q, setQ] = useState('');

  const load = async () => {
    setLoading(true);
    try {
      const env = await PlusAPI.getMemory({ limit: 100 });
      if (!env.success) return showError(env.message);
      setItems(env.data || []);
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

  const add = async () => {
    if (!form.content) return showError('内容不能为空');
    try {
      const env = await PlusAPI.addMemory(form);
      if (!env.success) return showError(env.message);
      showSuccess(env.message);
      setForm({ ...form, content: '' });
      load();
    } catch (e) {
      showError(e.message);
    }
  };

  const del = async (id) => {
    try {
      const env = await PlusAPI.deleteMemory(id);
      if (!env.success) return showError(env.message);
      load();
    } catch (e) {
      showError(e.message);
    }
  };

  const clear = async () => {
    try {
      const env = await PlusAPI.clearMemory();
      if (!env.success) return showError(env.message);
      load();
    } catch (e) {
      showError(e.message);
    }
  };

  const search = async () => {
    if (!q) return load();
    try {
      const env = await PlusAPI.searchMemory({ q, limit: 50 });
      if (!env.success) return showError(env.message);
      setItems(env.data || []);
    } catch (e) {
      showError(e.message);
    }
  };

  if (loading) return <Loader active inline='centered'>加载中</Loader>;

  return (
    <Container style={{ marginTop: '2em', marginBottom: '4em' }}>
      <Header as='h2'>Agent 记忆</Header>
      <p style={{ color: '#666' }}>short / long / profile 三级记忆，供 Agent 在会话间保持上下文。</p>

      <Segment>
        <div style={{ display: 'flex', gap: '0.5em', flexWrap: 'wrap', alignItems: 'center' }}>
          <Input placeholder='会话ID（可选）' value={form.session_id} onChange={(e, d) => setForm({ ...form, session_id: d.value })} style={{ width: '200px' }} />
          <Input placeholder='scope' value={form.scope} onChange={(e, d) => setForm({ ...form, scope: d.value })} style={{ width: '120px' }} />
          <Input placeholder='role' value={form.role} onChange={(e, d) => setForm({ ...form, role: d.value })} style={{ width: '100px' }} />
        </div>
        <TextArea
          placeholder='记忆内容'
          value={form.content}
          onChange={(e, d) => setForm({ ...form, content: d.value })}
          style={{ width: '100%', minHeight: '70px', marginTop: '0.5em' }}
        />
        <Button primary onClick={add} style={{ marginTop: '0.5em' }}>
          写入记忆
        </Button>
      </Segment>

      <div style={{ display: 'flex', gap: '0.5em', marginBottom: '1em', flexWrap: 'wrap' }}>
        <Input placeholder='搜索关键词' value={q} onChange={(e, d) => setQ(d.value)} style={{ width: '240px' }} />
        <Button onClick={search}>搜索</Button>
        <Button basic onClick={load}>
          刷新
        </Button>
        <Button negative basic onClick={clear}>
          清空
        </Button>
      </div>

      <Table celled striped>
        <Table.Header>
          <Table.Row>
            <Table.HeaderCell>ID</Table.HeaderCell>
            <Table.HeaderCell>Scope</Table.HeaderCell>
            <Table.HeaderCell>角色</Table.HeaderCell>
            <Table.HeaderCell>内容</Table.HeaderCell>
            <Table.HeaderCell>操作</Table.HeaderCell>
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {items.map((m) => (
            <Table.Row key={m.id}>
              <Table.Cell>{m.id}</Table.Cell>
              <Table.Cell>
                <Label size='small'>{m.scope}</Label>
              </Table.Cell>
              <Table.Cell>{m.role}</Table.Cell>
              <Table.Cell>{m.content}</Table.Cell>
              <Table.Cell>
                <Button size='mini' negative onClick={() => del(m.id)}>
                  删除
                </Button>
              </Table.Cell>
            </Table.Row>
          ))}
          {items.length === 0 && (
            <Table.Row>
              <Table.Cell colSpan='5' textAlign='center'>
                暂无记忆
              </Table.Cell>
            </Table.Row>
          )}
        </Table.Body>
      </Table>
    </Container>
  );
}
