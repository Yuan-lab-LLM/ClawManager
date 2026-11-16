import { useEffect, useRef, useState, type RefObject } from 'react';
import { ChevronLeft, ChevronRight, X } from 'lucide-react';
import ExpandableSearch from '../../../components/ExpandableSearch';
import FilterSelect from '../../../components/FilterSelect';
import { count, Gauge, percent } from './Charts';
import { percentOf } from './model';
import type { ClusterOverview, ResourceReading } from './types';
import type { OverviewText } from './text';
export default function NodesDialog({
  dialog,
  trigger,
  data,
  text,
  locale,
}: {
  dialog: RefObject<HTMLDialogElement | null>;
  trigger: RefObject<HTMLButtonElement | null>;
  data?: ClusterOverview;
  text: OverviewText;
  locale: string;
}) {
  const [search, setSearch] = useState(''),
    [status, setStatus] = useState('all'),
    [page, setPage] = useState(1),
    [selectedName, setSelectedName] = useState<string | null>(null),
    details = useRef<HTMLDialogElement>(null),
    selectedTrigger = useRef<HTMLButtonElement | null>(null);
  const rows = (data?.nodes ?? []).filter(
    (node) =>
      (status === 'all' || node.ready === (status === 'ready')) &&
      [node.name, node.internal_ip, ...node.roles].some((value) =>
        value
          .toLocaleLowerCase(locale)
          .includes(search.trim().toLocaleLowerCase(locale)),
      ),
  );
  const pages = Math.max(1, Math.ceil(rows.length / 10)),
    current = Math.min(page, pages),
    shown = rows.slice((current - 1) * 10, current * 10);
  const selected =
    data?.nodes.find((node) => node.name === selectedName) ?? null;
  useEffect(() => {
    if (selectedName && !selected) details.current?.close();
    else if (selected && details.current && !details.current.open)
      details.current.showModal();
  }, [selected, selectedName]);
  const resource = (reading: ResourceReading) =>
    `${percent(percentOf(reading.used, reading.capacity), locale)} / ${percent(percentOf(reading.requested, reading.allocatable), locale)}`;
  return (
    <>
      <dialog
        ref={dialog}
        className="ao-dialog ao-nodes-dialog"
        aria-labelledby="ao-nodes-title"
        onClose={() => trigger.current?.focus()}
      >
        <div className="ao-dialog-head">
          <h2 id="ao-nodes-title">{text.nodeDetails}</h2>
          <button
            type="button"
            className="cm-icon-button"
            aria-label={text.close}
            onClick={() => dialog.current?.close()}
          >
            <X size={18} />
          </button>
        </div>
        <div className="ao-nodes-tools">
          <span>
            {text.count} {count(data?.node_count, locale)}
          </span>
          <div>
            <ExpandableSearch
              value={search}
              label={text.search}
              placeholder={text.searchPlaceholder}
              onChange={(value) => {
                setSearch(value);
                setPage(1);
              }}
            />
            <FilterSelect
              label={text.status}
              value={status}
              onChange={(value) => {
                setStatus(value);
                setPage(1);
              }}
              options={[
                { value: 'all', label: text.all },
                { value: 'ready', label: text.ready },
                { value: 'not-ready', label: text.notReady },
              ]}
            />
          </div>
        </div>
        <div
          className="ao-table-scroll"
          tabIndex={0}
          aria-label={text.nodeDetails}
        >
          <table>
            <thead>
              <tr>
                <th>{text.node}</th>
                <th>{text.status}</th>
                <th>{text.role}</th>
                <th>{text.ip}</th>
                <th>{text.pods}</th>
                <th>
                  {text.cpu} · {text.actual} / {text.requested}
                </th>
                <th>
                  {text.memory} · {text.actual} / {text.requested}
                </th>
                <th>
                  {text.disk} · {text.actual} / {text.requested}
                </th>
                <th>{text.details}</th>
              </tr>
            </thead>
            <tbody>
              {shown.map((node) => (
                <tr key={node.name}>
                  <td>
                    <span tabIndex={0} title={node.name}>
                      {node.name}
                    </span>
                  </td>
                  <td>
                    <span className={node.ready ? 'ao-good' : 'ao-warning'}>
                      {node.ready ? text.ready : text.notReady}
                    </span>
                  </td>
                  <td title={node.roles.join(', ')}>{node.roles.join(', ')}</td>
                  <td>{node.internal_ip || '—'}</td>
                  <td>{count(node.pod_count, locale)}</td>
                  <td>{resource(node.cpu)}</td>
                  <td>{resource(node.memory)}</td>
                  <td>{resource(node.disk)}</td>
                  <td>
                    <button
                      className="ao-link"
                      aria-label={`${node.name} · ${text.details}`}
                      onClick={(event) => {
                        selectedTrigger.current = event.currentTarget;
                        setSelectedName(node.name);
                      }}
                    >
                      {text.details}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {!shown.length && (
            <p className="ao-empty" role="status">
              {data ? text.noMatches : text.resourceUnavailable}
            </p>
          )}
        </div>
        <div className="ao-dialog-foot">
          <span>
            {count(current, locale)} / {count(pages, locale)}
          </span>
          <div>
            <button
              className="cm-icon-button"
              aria-label={text.previous}
              disabled={current <= 1}
              onClick={() => setPage(current - 1)}
            >
              <ChevronLeft size={16} />
            </button>
            <button
              className="cm-icon-button"
              aria-label={text.next}
              disabled={current >= pages}
              onClick={() => setPage(current + 1)}
            >
              <ChevronRight size={16} />
            </button>
          </div>
        </div>
      </dialog>
      <dialog
        ref={details}
        className="ao-dialog ao-node-detail"
        aria-label={selected?.name ?? text.details}
        onClose={() => {
          setSelectedName(null);
          selectedTrigger.current?.focus();
        }}
      >
        <div className="ao-dialog-head">
          <h2>{selected?.name}</h2>
          <button
            className="cm-icon-button"
            aria-label={text.close}
            onClick={() => details.current?.close()}
          >
            <X size={18} />
          </button>
        </div>
        {selected && (
          <>
            <p className="ao-node-facts">
              {selected.internal_ip} · {selected.roles.join(', ')} ·{' '}
              {selected.kubelet_version}
            </p>
            <div className="ao-resource-grid">
              {(['cpu', 'memory', 'disk'] as const).map((key) => (
                <Gauge
                  key={key}
                  value={selected[key]}
                  label={text[key]}
                  unit={key === 'cpu' ? text.cores : 'GiB'}
                  locale={locale}
                  text={text}
                />
              ))}
            </div>
          </>
        )}
      </dialog>
    </>
  );
}
