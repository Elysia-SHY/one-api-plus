import React, { useEffect, useState } from 'react';
import { Container, Header, Loader, Table, Label, Message } from 'semantic-ui-react';
import PlusAPI from '../../helpers/plus';
import { showError } from '../../helpers/utils';

export default function RateLimit() {
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(false);

  const load = async () => {
    setLoading(true);
    try {
      const env = await PlusAPI.getRateLimit();
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
      <Header as='h2'>限流状态</Header>
      <p style={{ color: '#666' }}>当前用户/令牌的 QPM、并发闸门与实时在途请求数。</p>
      {data && !data.enabled && (
        <Message warning>限流未启用（设置 RATE_LIMIT_ENABLED=true 后生效）。</Message>
      )}
      {data && (
        <Table definition>
          <Table.Body>
            <Table.Row>
              <Table.Cell>是否启用</Table.Cell>
              <Table.Cell>{data.enabled ? <Label color='green'>是</Label> : <Label>否</Label>}</Table.Cell>
            </Table.Row>
            <Table.Row>
              <Table.Cell>QPM 上限</Table.Cell>
              <Table.Cell>{data.qpm}</Table.Cell>
            </Table.Row>
            <Table.Row>
              <Table.Cell>突发额度</Table.Cell>
              <Table.Cell>{data.burst}</Table.Cell>
            </Table.Row>
            <Table.Row>
              <Table.Cell>每模型独立限流</Table.Cell>
              <Table.Cell>{data.per_model ? '是' : '否'}</Table.Cell>
            </Table.Row>
            <Table.Row>
              <Table.Cell>并发上限</Table.Cell>
              <Table.Cell>{data.concurrent}</Table.Cell>
            </Table.Row>
            <Table.Row>
              <Table.Cell>当前在途请求</Table.Cell>
              <Table.Cell>
                <Label color='blue'>{data.in_flight}</Label>
              </Table.Cell>
            </Table.Row>
          </Table.Body>
        </Table>
      )}
    </Container>
  );
}
