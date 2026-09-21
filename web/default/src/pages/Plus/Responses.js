import React, { useEffect, useState } from 'react';
import { Container, Header, Loader, Table, Label, Message } from 'semantic-ui-react';
import PlusAPI from '../../helpers/plus';
import { showError } from '../../helpers/utils';

export default function Responses() {
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(false);

  const load = async () => {
    setLoading(true);
    try {
      const env = await PlusAPI.getResponsesStatus();
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

  if (loading) return <Loader active inline='centered'>加载中</Loader>;

  return (
    <Container style={{ marginTop: '2em', marginBottom: '4em' }}>
      <Header as='h2'>Responses API</Header>
      <p style={{ color: '#666' }}>新版 OpenAI Responses 兼容层（/v1/responses）的运行状态。</p>
      {data && !data.enabled && <Message warning>Responses API 未启用（设置 RESPONSES_API_ENABLED=true 后生效）。</Message>}
      {data && (
        <Table definition>
          <Table.Body>
            <Table.Row>
              <Table.Cell>是否启用</Table.Cell>
              <Table.Cell>{data.enabled ? <Label color='green'>是</Label> : <Label>否</Label>}</Table.Cell>
            </Table.Row>
            <Table.Row>
              <Table.Cell>端点</Table.Cell>
              <Table.Cell>{data.endpoint}</Table.Cell>
            </Table.Row>
            <Table.Row>
              <Table.Cell>存储条目上限</Table.Cell>
              <Table.Cell>{data.store_items}</Table.Cell>
            </Table.Row>
          </Table.Body>
        </Table>
      )}
    </Container>
  );
}
