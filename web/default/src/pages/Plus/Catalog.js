import React, { useEffect, useState } from 'react';
import {
  Container,
  Header,
  Button,
  Table,
  Message,
  Loader,
  Input,
  Label,
  Popup,
} from 'semantic-ui-react';
import PlusAPI from '../../helpers/plus';
import { showError, showSuccess } from '../../helpers/utils';

const STATUS_TEXT = {
  new: '新发现',
  normal: '正常',
  degraded: '降级',
  paused: '已暂停',
  invalid: '无效',
};
const STATUS_COLOR = {
  new: 'blue',
  normal: 'green',
  degraded: 'yellow',
  paused: 'orange',
  invalid: 'red',
};

export default function Catalog() {
  const [items, setItems] = useState([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [keyword, setKeyword] = useState('');
  const [syncing, setSyncing] = useState(false);
  const [result, setResult] = useState(null);

  const load = async (kw = keyword) => {
    setLoading(true);
    try {
      const env = await PlusAPI.getCatalog({ keyword: kw, p: 0, page_size: 100 });
      if (!env.success) {
        showError(env.message);
        return;
      }
      setItems(env.data.items || []);
      setTotal(env.data.total || 0);
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

  const syncAll = async () => {
    setSyncing(true);
    setResult(null);
    try {
      const env = await PlusAPI.syncModels();
      if (!env.success) {
        showError(env.message);
        return;
      }
      setResult(env.data);
      showSuccess(`同步完成，新增 ${env.data.new_models} 个模型`);
      load();
    } catch (e) {
      showError(e.message);
    } finally {
      setSyncing(false);
    }
  };

  const enable = async (it) => {
    try {
      const env = await PlusAPI.enableModel({
        channel_id: it.channel_id,
        model: it.model_name,
      });
      if (!env.success) return showError(env.message);
      showSuccess(`已启用 ${it.model_name}`);
      load();
    } catch (e) {
      showError(e.message);
    }
  };

  const setStatus = async (it, status) => {
    try {
      const env = await PlusAPI.updateCatalogModel({
        channel_id: it.channel_id,
        model: it.model_name,
        status,
      });
      if (!env.success) return showError(env.message);
      load();
    } catch (e) {
      showError(e.message);
    }
  };

  return (
    <Container style={{ marginTop: '2em', marginBottom: '4em' }}>
      <Header as='h2'>模型目录 / 自动发现</Header>
      <p style={{ color: '#666' }}>
        调用各上游 <code>/v1/models</code> 自动发现模型并登记。可在此触发全量同步、启用模型或调整状态。
      </p>
      <div style={{ display: 'flex', gap: '0.5em', marginBottom: '1em', flexWrap: 'wrap' }}>
        <Input
          placeholder='搜索模型名 / 来源'
          value={keyword}
          onChange={(e, d) => setKeyword(d.value)}
          onKeyPress={(e) => e.key === 'Enter' && load()}
          style={{ width: '260px' }}
        />
        <Button onClick={() => load()}>查询</Button>
        <Button primary loading={syncing} onClick={syncAll}>
          立即同步全部渠道
        </Button>
      </div>

      {result && (
        <Message positive={!(result.channels || []).some((c) => c.error)} warning={(result.channels || []).some((c) => c.error)}>
          本次同步：{result.channels?.length || 0} 个渠道，新增 <b>{result.new_models}</b> 个模型。
          {(result.channels || []).some((c) => c.error) && (
            <Message.List style={{ marginTop: '0.5em' }}>
              {(result.channels || [])
                .filter((c) => c.error)
                .map((c, i) => (
                  <Message.Item key={i}>
                    渠道 #{c.channel_id}（{c.channel}）：{c.error}
                  </Message.Item>
                ))}
            </Message.List>
          )}
        </Message>
      )}

      {loading ? (
        <Loader active inline='centered'>
          加载中
        </Loader>
      ) : (
        <Table celled striped>
          <Table.Header>
            <Table.Row>
              <Table.HeaderCell>渠道ID</Table.HeaderCell>
              <Table.HeaderCell>模型</Table.HeaderCell>
              <Table.HeaderCell>来源</Table.HeaderCell>
              <Table.HeaderCell>状态</Table.HeaderCell>
              <Table.HeaderCell>启用</Table.HeaderCell>
              <Table.HeaderCell>操作</Table.HeaderCell>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {items.map((it, i) => (
              <Table.Row key={i}>
                <Table.Cell>{it.channel_id}</Table.Cell>
                <Table.Cell>{it.model_name}</Table.Cell>
                <Table.Cell>{it.source}</Table.Cell>
                <Table.Cell>
                  <Label color={STATUS_COLOR[it.status] || 'grey'} size='small'>
                    {STATUS_TEXT[it.status] || it.status}
                  </Label>
                </Table.Cell>
                <Table.Cell>{it.enabled ? '是' : '否'}</Table.Cell>
                <Table.Cell>
                  <Button size='mini' positive onClick={() => enable(it)}>
                    启用
                  </Button>
                  <Popup
                    on='click'
                    position='bottom left'
                    trigger={
                      <Button size='mini' basic>
                        改状态
                      </Button>
                    }
                    content={
                      <div style={{ display: 'flex', gap: '0.3em', flexWrap: 'wrap' }}>
                        {Object.keys(STATUS_TEXT).map((s) => (
                          <Button
                            key={s}
                            size='mini'
                            color={STATUS_COLOR[s]}
                            onClick={() => setStatus(it, s)}
                          >
                            {STATUS_TEXT[s]}
                          </Button>
                        ))}
                      </div>
                    }
                  />
                </Table.Cell>
              </Table.Row>
            ))}
            {items.length === 0 && (
              <Table.Row>
                <Table.Cell colSpan='6' textAlign='center'>
                  暂无数据，点「立即同步全部渠道」拉取上游模型。
                </Table.Cell>
              </Table.Row>
            )}
          </Table.Body>
        </Table>
      )}
      <p style={{ color: '#999' }}>共 {total} 条</p>
    </Container>
  );
}
