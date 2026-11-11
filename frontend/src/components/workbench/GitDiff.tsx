import { useEffect, useState } from 'react';
import { useI18n } from '../../contexts/I18nContext';
import { useWorkbenchServices } from './WorkbenchServices';
import { workbenchError } from '../../services/instanceWorkbenchService';
import { parseUnifiedDiff, safeImageMime } from './gitPresentation';

export function GitDiff({ instanceId, imagePath, diff, binary }: {
  instanceId: number; imagePath: string | null; diff: string; binary: boolean;
}) {
  const { t } = useI18n();
  const { workspace: workspaceService } = useWorkbenchServices();
  const [image, setImage] = useState<{ url?: string; error?: string }>({});
  useEffect(() => {
    let active = true, objectURL: string | undefined;
    if (imagePath) {
      workspaceService.previewBlob(instanceId, imagePath).then(blob => {
        if (!active) return;
        if (!safeImageMime(blob.type)) { setImage({ error: t('workbench.unsupportedBinary') }); return; }
        objectURL = URL.createObjectURL(blob);
        setImage({ url: objectURL });
      }).catch(error => { if (active) setImage({ error: workbenchError(error) }); });
    }
    return () => { active = false; if (objectURL) URL.revokeObjectURL(objectURL); };
  }, [instanceId, imagePath, workspaceService, t]);
  if (imagePath) return <div className="wb-image-preview">
    <p>{t('workbench.workingCopyPreview')}</p>
    {image.error ? <div className="wb-error" role="alert">{image.error}</div> : image.url ?
      <img src={image.url} alt={imagePath} onError={() => setImage({error:t('workbench.imageLoadError')})} /> :
      <div className="wb-loading" role="status">{t('workbench.loading')}</div>}
  </div>;
  if (binary) return <div className="wb-diff-placeholder">{t('workbench.unsupportedBinary')}</div>;
  const rows = parseUnifiedDiff(diff).filter(line => line.kind !== "meta" || line.text.startsWith("\\ No newline"));
  if (!rows.length) return <div className="wb-diff-placeholder">{t('workbench.noDiff')}</div>;
  const additions = rows.filter(row => row.kind === 'addition').length;
  const deletions = rows.filter(row => row.kind === 'deletion').length;
  return <><div className="wb-diff-summary">{t('workbench.diffLines')} <b>+{additions}</b> −{deletions}</div>
    <div className="wb-diff" role="region" aria-label={t('workbench.diff')} tabIndex={0}>
      {rows.map((line, i) => <div key={i} className={`wb-diff-line ${line.kind}`}>
        <span className="wb-diff-number" aria-label={line.oldLine ? t('workbench.oldLine') : undefined}>{line.oldLine}</span>
        <span className="wb-diff-number" aria-label={line.newLine ? t('workbench.newLine') : undefined}>{line.newLine}</span>
        <code>{line.text}</code>
      </div>)}
    </div></>;
}
