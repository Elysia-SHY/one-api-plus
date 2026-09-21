import React, { useEffect, useState } from 'react';
import {
  Container,
  Header,
  Loader,
  Table,
  Button,
  Input,
  Label,
  Segment,
  Message,
  Grid,
} from 'semantic-ui-react';
import PlusAPI from '../../helpers/plus';
import { showError, showSuccess } from '../../helpers/utils';

export default function Dashboard() {
  const [dash, setDash] = useState(null);
  const [load, setLoad] = useState(null);
  const [routing, setRouting] = useState(null);
  const [weights, setWeights] = useState({ cost: 0, latency: 0, load: 0, stability: 0 });
  const [loading, setLoading] = useState(false);

  const loadAll = async () => {
    setLoading(true);
    try {
      const [d, l, r] = await Promise.all([
        PlusAPI.getDashboard().catch(() => null),
        PlusAPI.getLoad().catch(() => null),
        PlusAPI.getRouting().catch(() => null),
      ]);
      if (d && d.success) setDash(d.data);
      else if (d) showError(d.message);
      if (l && l.success) setLoad(l.data);
      if (r && r.success) {
        setRouting(r.data);
        const w = r.data.weights || {};
        setWeights({ cost: w.cost || 0, latency: w.latency || 0, load: w.load || 0, stability: w.stability || 0 });
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

  const saveWeights = async () => {
    try {
      const env = await PlusAPI.updateRouting({ weights });
      if (!env.success) return showError(env.message);
      showSuccess('已更新路由权重');
      loadAll();
    } catch (e) {
      showError(e.message);
    }
  };

  if (loading) return <Loader active inline='centered'>加载中</Loader>;

  return (
    <Container style={{ marginTop: '2em', marginBottom: '4em' }}>
      <Header as='h2'>仪表盘 / 路由</Header>
      <p style={{ color: '#666' }}>用量与成本概览、各渠道实时并发负载，以及智能路由评分权重调节。</p>

      {dash && (
        <Segment>
          <Header as='h4'>概览</Header>
          <pre style={{ whiteSpace: 'pre-wrap', fontSize: '13px' }}>{JSON.stringify(dash, null, 2)}</pre>
        </Segment>
      )}

      <Header as='h3'>实时负载</Header>
      {load ? (
        <Table celled striped>
          <Table.Header>
            <Table.Row>
              <Table.HeaderCell>渠道 ID</Table.HeaderCell>
              <Table.HeaderCell>当前在途请求</Table.HeaderCell>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {Object.entries(load.channels || {}).map(([cid, inflight]) => (
              <Table.Row key={cid}>
                <Table.Cell>{cid}</Table.Cell>
                <Table.Cell>
                  <Label color={inflight > 0 ? 'blue' : 'grey'}>{inflight}</Label>
                </Table.Cell>
              </Table.Row>
            ))}
            {Object.keys(load.channels || {}).length === 0 && (
              <Table.Row>
                <Table.Cell colSpan='2' textAlign='center'>
                  当前无在途请求
                </Table.Cell>
              </Table.Row>
            )}
            <Table.Row>
              <Table.Cell>
                <b>总计</b>
              </Table.Cell>
              <Table.Cell>{load.total}</Table.Cell>
            </Table.Row>
          </Table.Body>
        </Table>
      ) : (
        <Message>无负载数据</Message>
      )}

      <Header as='h3'>路由评分权重</Header>
      {routing && (
        <Segment>
          <p style={{ color: '#666' }}>
            当前策略：<Label>{routing.strategy}</Label>　负载感知：{routing.loadAware ? '开' : '关'}　最大负载：{routing.maxLoad}
          </p>
          <Grid columns={4} stackable>
            <Grid.Column>
              <label>成本</label>
              <Input
                type='number'
                step='0.01'
                value={weights.cost}
                onChange={(e, d) => setWeights({ ...weights, cost: Number(d.value) })}
              />
            </Grid.Column>
            <Grid.Column>
              <label>延迟</label>
              <Input
                type='number'
                step='0.01'
                value={weights.latency}
                onChange={(e, d) => setWeights({ ...weights, latency: Number(d.value) })}
              />
            </Grid.Column>
            <Grid.Column>
              <label>负载</label>
              <Input
                type='number'
                step='0.01'
                value={weights.load}
                onChange={(e, d) => setWeights({ ...weights, load: Number(d.value) })}
              />
            </Grid.Column>
            <Grid.Column>
              <label>稳定性</label>
              <Input
                type='number'
                step='0.01'
                value={weights.stability}
                onChange={(e, d) => setWeights({ ...weights, stability: Number(d.value) })}
              />
            </Grid.Column>
          </Grid>
          <Button primary style={{ marginTop: '1em' }} onClick={saveWeights}>
            保存权重
          </Button>
        </Segment>
      )}
    </Container>
  );
}
