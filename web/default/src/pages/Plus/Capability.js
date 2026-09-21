import React, { useEffect, useState } from 'react';
import {
  Container,
  Header,
  Table,
  Loader,
  Button,
  Checkbox,
  Input,
  Label,
  Message,
} from 'semantic-ui-react';
import PlusAPI from '../../helpers/plus';
import { showError, showSuccess } from '../../helpers/utils';

const cap = (v) =>
  v === 1 || v === true ? (
    <Label color='green' size='small'>
      支持
    </Label>
  ) : (
    <Label color='grey' size='small'>
      否
    </Label>
  );

export default function Capability() {
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(false);
  const [filters, setFilters] = useState({ context: 0, vision: false, tool_call: false, reasoning: false, embedding: false });
  const [q, setQ] = useState('');

  const load = async () => {
    setLoading(true);
    try {
      const env = await PlusAPI.getCapabilities();
      if (!env.success) return showError(env.message);
      setItems(env.data || []);
    } catch (e) {
      showError(e.message);
    } finally {
      setLoading(false);
    }
  };

  const search = async () => {
    setLoading(true);
    try {
      const env = await PlusAPI.searchCapabilities({
        context: filters.context,
        vision: filters.vision,
        tool_call: filters.tool_call,
        reasoning: filters.reasoning,
        embedding: filters.embedding,
      });
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

  const refresh = async (name) => {
    try {
      const env = await PlusAPI.refreshCapability(name, true);
      if (!env.success) return showError(env.message);
      showSuccess(`已刷新 ${name}`);
      load();
    } catch (e) {
      showError(e.message);
    }
  };

  return (
    <Container style={{ marginTop: '2em', marginBottom: '4em' }}>
      <Header as='h2'>模型能力库</Header>
      <p style={{ color: '#666' }}>
        记录每个模型的 context_length / vision / tool_call / reasoning / embedding 能力，供模型组按能力选路。
      </p>
      <div style={{ display: 'flex', gap: '0.5em', marginBottom: '1em', flexWrap: 'wrap', alignItems: 'center' }}>
        <Input placeholder='搜索模型名' value={q} onChange={(e, d) => setQ(d.value)} style={{ width: '220px' }} />
        <Checkbox label='视觉' checked={filters.vision} onChange={(e, d) => setFilters({ ...filters, vision: d.checked })} />
        <Checkbox label='工具调用' checked={filters.tool_call} onChange={(e, d) => setFilters({ ...filters, tool_call: d.checked })} />
        <Checkbox label='推理' checked={filters.reasoning} onChange={(e, d) => setFilters({ ...filters, reasoning: d.checked })} />
        <Checkbox label='嵌入' checked={filters.embedding} onChange={(e, d) => setFilters({ ...filters, embedding: d.checked })} />
        <Button onClick={search}>按能力筛选</Button>
        <Button basic onClick={load}>
          全部
        </Button>
      </div>

      {loading ? (
        <Loader active inline='centered'>
          加载中
        </Loader>
      ) : (
        <Table celled striped>
          <Table.Header>
            <Table.Row>
              <Table.HeaderCell>模型</Table.HeaderCell>
              <Table.HeaderCell>上下文长度</Table.HeaderCell>
              <Table.HeaderCell>视觉</Table.HeaderCell>
              <Table.HeaderCell>工具调用</Table.HeaderCell>
              <Table.HeaderCell>推理</Table.HeaderCell>
              <Table.HeaderCell>嵌入</Table.HeaderCell>
              <Table.HeaderCell>操作</Table.HeaderCell>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {items.map((it, i) => (
              <Table.Row key={i}>
                <Table.Cell>{it.model_name}</Table.Cell>
                <Table.Cell>{it.context_length || '-'}</Table.Cell>
                <Table.Cell>{cap(it.vision)}</Table.Cell>
                <Table.Cell>{cap(it.tool_call)}</Table.Cell>
                <Table.Cell>{cap(it.reasoning)}</Table.Cell>
                <Table.Cell>{cap(it.embedding)}</Table.Cell>
                <Table.Cell>
                  <Button size='mini' onClick={() => refresh(it.model_name)}>
                    重新推断
                  </Button>
                </Table.Cell>
              </Table.Row>
            ))}
            {items.length === 0 && (
              <Table.Row>
                <Table.Cell colSpan='7' textAlign='center'>
                  暂无能力记录
                </Table.Cell>
              </Table.Row>
            )}
          </Table.Body>
        </Table>
      )}
    </Container>
  );
}
