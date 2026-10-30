import CodeMirror from '@uiw/react-codemirror';
import { javascript } from '@codemirror/lang-javascript';
import { sql } from '@codemirror/lang-sql';
import { markdown } from '@codemirror/lang-markdown';
import { usePreferences } from '../ui/preferences';
export function CodeEditor({value='',onChange,language='javascript',height='360px',readOnly=false}:{value?:string;onChange?:(value:string)=>void;language?:'javascript'|'sql'|'markdown'|'json';height?:string;readOnly?:boolean}){
 const {dark}=usePreferences();
 return <div className="code-editor"><CodeMirror aria-label={`${language} 代码编辑器`} value={value} height={height} theme={dark?'dark':'light'} extensions={[language==='sql'?sql():language==='markdown'?markdown():javascript()]} onChange={onChange} readOnly={readOnly} basicSetup={{lineNumbers:true,foldGutter:true,highlightActiveLine:true,autocompletion:true}}/></div>;
}
