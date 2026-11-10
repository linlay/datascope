import { useState } from 'react';
import { BookOutlined } from '@ant-design/icons';
import { Button, Drawer, Select } from 'antd';
import Markdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { usePreferences } from '../ui/preferences';
import chinese from '../help/guide.zh-CN.md?raw';
import english from '../help/guide.en.md?raw';

export function UserGuide({ scriptReference = false }: { scriptReference?: boolean }) {
  const { language } = usePreferences();
  const en = language === 'en';
  const title = en ? 'User guide' : '使用帮助';
  const chapters = (en ? english : chinese).split(/^## /m).slice(1).map(section => {
    const end = section.indexOf('\n');
    return { title: section.slice(0, end).trim(), body: section.slice(end).trim() };
  });
  const [open, setOpen] = useState(false);
  const [chapter, setChapter] = useState(0);
  function showGuide() {
    setChapter(scriptReference ? Math.max(0, chapters.findIndex(item => item.body.includes('`my.one`'))) : 0);
    setOpen(true);
  }
  return <>
    <Button type={scriptReference ? 'link' : 'default'} icon={<BookOutlined />} onClick={showGuide}>
      {scriptReference ? (en ? 'Script reference' : '脚本接口手册') : title}
    </Button>
    <Drawer title={title} open={open} onClose={() => setOpen(false)} size="min(720px, 100vw)">
      <Select aria-label={en ? 'Chapter' : '选择章节'} style={{ width: '100%' }}
        value={chapter} onChange={setChapter}
        options={chapters.map((item, value) => ({ value, label: item.title }))} />
      <article className="user-guide" key={chapter}>
        <h2>{chapters[chapter].title}</h2>
        <Markdown remarkPlugins={[remarkGfm]}>{chapters[chapter].body}</Markdown>
      </article>
    </Drawer>
  </>;
}
