import type { Locale } from '../lib/i18n';
const labels = {
 en: { manage:"Instance management", hideList:"Hide instance list", showList:"Show instance list", rename:'Rename instance', name:'Instance name', length:'Use 3–50 characters.', failed:'Could not rename this instance.', list:'Instances', resizeList:'Resize instance list' },
 zh: { manage:"实例管理", hideList:"隐藏实例列表", showList:"展开实例列表", rename:'重命名实例', name:'实例名称', length:'名称需为 3–50 个字符。', failed:'暂时无法重命名实例。', list:'实例列表', resizeList:'调整实例列表宽度' },
 ja: { manage:"インスタンス管理", hideList:"インスタンス一覧を非表示", showList:"インスタンス一覧を表示", rename:'インスタンス名を変更', name:'インスタンス名', length:'3–50 文字で入力してください。', failed:'名前を変更できませんでした。', list:'インスタンス', resizeList:'インスタンス一覧の幅を調整' },
 ko: { manage:"인스턴스 관리", hideList:"인스턴스 목록 숨기기", showList:"인스턴스 목록 표시", rename:'인스턴스 이름 변경', name:'인스턴스 이름', length:'3–50자로 입력하세요.', failed:'이름을 변경할 수 없습니다.', list:'인스턴스', resizeList:'인스턴스 목록 너비 조정' },
 de: { manage:"Instanzverwaltung", hideList:"Instanzliste ausblenden", showList:"Instanzliste anzeigen", rename:'Instanz umbenennen', name:'Instanzname', length:'Verwenden Sie 3–50 Zeichen.', failed:'Instanz konnte nicht umbenannt werden.', list:'Instanzen', resizeList:'Breite der Instanzliste ändern' },
};
export const instanceWorkspaceText = (locale: Locale) => labels[locale];
