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
  TextArea,
  Message,
} from 'semantic-ui-react';
import PlusAPI from '../../helpers/plus';
import { showError, showSuccess } from '../../helpers/utils';

export default function Mcp() {
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(false);
  const [form, setForm] = useState({ name: '', url: '', enabled: true });
  const [tool, setTool] = useState({ tool: '', arguments: '{}' });
  const [callResult, setCallResult] = useState(null);

  const load = async () => {
    setLoading(true);
    try {
      const env = await PlusAPI.getMCPServers();
      if (!env.success) return showError(env.message);
      setData(env.data);
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

  const upsert = async () => {
    if (!form.name || !form.url) return showError('name 与 url 不能为空');
    try {
      const env = await PlusAPI.upsertMCPServer(form);
      if (!env.success) return showError(env.message);
      showSuccess('已保存 MCP 服务器');
      setForm({ name: '', url: '', enabled: true });
      load();
    } catch (e) {
      showError(e.message);
    }
  };

  const remove = async (id) => {
    try {
      const env = await PlusAPI.removeMCPServer(id);
      if (!env.success) return showError(env.message);
      load();
    } catch (e) {
      showError(e.message);
    }
  };

  const discover = async () => {
    try {
      const env = await PlusAPI.discoverMCPTools();
      if (!env.success) return showError(env.message);
      showSuccess(env.message);
      load();
    } catch (e) {
      showError(e.message);
    }
  };

  const call = async () => {
    try {
      const env = await PlusAPI.callMCPTool({ tool: tool.tool, arguments: tool.arguments });
      if (!env.success) return showError(env.message);
      setCallResult(env.data);
    } catch (e) {
      showError(e.message);
    }
  };

  if (loading) return <Loader active inline='centered'>加载中</Loader>;

  return (
    <Container style={{ marginTop: '2em', marginBottom: '4em' }}>
      <Header as='h2'>MCP 网关</Header>
      <p style={{ color: '#666' }}>接入 Model Context Protocol 服务器，自动发现工具并供 Agent 调用。</p>

      <Segment>
        <Header as='h4'>新增 / 更新服务器</Header>
        <div style={{ display: 'flex', gap: '0.5em', flexWrap: 'wrap', alignItems: 'center' }}>
          <Input placeholder='名称' value={form.name} onChange={(e, d) => setForm({ ...form, name: d.value })} style={{ width: '160px' }} />
          <Input placeholder='URL（如 http://host:port/mcp）' value={form.url} onChange={(e, d) => setForm({ ...form, url: d.value })} style={{ width: '320px' }} />
          <Button primary onClick={upsert}>
            保存
          </Button>
          <Button onClick={discover}>发现全部工具</Button>
        </div>
      </Segment>

      <Header as='h4'>已注册服务器</Header>
      <Table celled striped>
        <Table.Header>
          <Table.Row>
            <Table.HeaderCell>名称</Table.HeaderCell>
            <Table.HeaderCell>URL</Table.HeaderCell>
            <Table.HeaderCell>工具数</Table.HeaderCell>
            <Table.HeaderCell>操作</Table.HeaderCell>
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {(data?.servers || []).map((s, i) => (
            <Table.Row key={i}>
              <Table.Cell>{s.name}</Table.Cell>
              <Table.Cell>{s.url}</Table.Cell>
              <Table.Cell>
                <Label color='blue'>{s.tools?.length || 0}</Label>
              </Table.Cell>
              <Table.Cell>
                <Button size='mini' negative onClick={() => remove(s.id)}>
                  删除
                </Button>
              </Table.Cell>
            </Table.Row>
          ))}
          {(!data?.servers || data.servers.length === 0) && (
            <Table.Row>
              <Table.Cell colSpan='4' textAlign='center'>
                暂无 MCP 服务器
              </Table.Cell>
            </Table.Row>
          )}
        </Table.Body>
      </Table>

      <Header as='h4'>全局工具清单（{data?.tools?.length || 0}）</Header>
      <div>
        {(data?.tools || []).map((t, i) => (
          <Label key={i} style={{ margin: '2px' }}>
            {t}
          </Label>
        ))}
      </div>

      <Segment style={{ marginTop: '1em' }}>
        <Header as='h4'>调用工具</Header>
        <div style={{ display: 'flex', gap: '0.5em', flexWrap: 'wrap', alignItems: 'flex-start' }}>
          <Input placeholder='工具名' value={tool.tool} onChange={(e, d) => setTool({ ...tool, tool: d.value })} style={{ width: '220px' }} />
          <TextArea
            placeholder='参数 JSON'
            value={tool.arguments}
            onChange={(e, d) => setTool({ ...tool, arguments: d.value })}
            style={{ width: '320px', minHeight: '60px' }}
          />
          <Button onClick={call}>调用</Button>
        </div>
        {callResult && <Message info>{JSON.stringify(callResult)}</Message>}
      </Segment>
    </Container>
  );
}
