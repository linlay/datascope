const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const vm=require('node:vm');
function host(){const app={children:[],append(n){this.children.push(n)},replaceChildren(){this.children=[]}};let receive;let queries=0;const window={viewHost:{onData(fn){receive=fn},query(){queries++;throw Error('confirmation view must not call MCP')}}};const document={getElementById(){return app},createElement(tag){return {tag,style:{},textContent:''}}};vm.runInNewContext(fs.readFileSync(path.join(__dirname,'../datascope/views/action.js'),'utf8'),{window,document,URL});return {app,receive,get queries(){return queries}}}
test('confirmation view only displays a preview and opens the human authentication page',()=>{
 const h=host();const token='a'.repeat(64);h.receive({title:'Review',preview:{result:{name:'<script>alert(1)</script>'},writes:[{sql:'UPDATE records',rows:1}]},confirmation_url:'https://data.example.test/actions/confirm?token='+token});
 const link=h.app.children.find(n=>n.tag==='a');assert(link);assert.equal(link.target,'_blank');assert.equal(link.rel,'noopener noreferrer');assert.equal(h.queries,0);assert(h.app.children.find(n=>n.tag==='pre').textContent.includes('<script>'));
});
test('confirmation view rejects script URLs, unrelated paths and malformed tokens',()=>{
 for(const url of ['javascript:alert(1)','https://example.test/login?token='+'a'.repeat(64),'https://user:pass@example.test/actions/confirm?token='+'a'.repeat(64),'https://example.test/actions/confirm?token=bad']){const h=host();h.receive({preview:{},confirmation_url:url});assert.equal(h.app.children.length,0);assert.equal(h.app.textContent,'确认链接无效');assert.equal(h.queries,0)}
});
test('confirmation manifest contains no MCP route that could approve a request',()=>{
 const manifest=JSON.parse(fs.readFileSync(path.join(__dirname,'../datascope/view.json'),'utf8'));const action=manifest.views.action;assert.equal(action.queries,undefined);for(const name of [action.entry,...action.assets])assert(fs.existsSync(path.join(__dirname,'../datascope',name)));assert.equal(action.dataSchema.additionalProperties,false)
});
