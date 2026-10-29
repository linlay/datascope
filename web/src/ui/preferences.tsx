import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { ConfigProvider, theme, type ThemeConfig } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import enUS from 'antd/locale/en_US';
import { english } from './translations';

export type Language = 'zh-CN' | 'en';
export type Appearance = 'system' | 'light' | 'dark';
export type Translate = (message:string,params?:Record<string,unknown>)=>string;
interface Preferences {language:Language;appearance:Appearance;dark:boolean;setLanguage:(value:Language)=>void;setAppearance:(value:Appearance)=>void;t:Translate}
const PreferencesContext=createContext<Preferences|null>(null);
export function usePreferences(){const value=useContext(PreferencesContext);if(!value)throw new Error('PreferencesProvider is required');return value}
export function useI18n(){return usePreferences().t}
function readLanguage():Language{return localStorage.getItem('datascope-language')==='en'?'en':'zh-CN'}
function readAppearance():Appearance{const value=localStorage.getItem('datascope-theme');return value==='light'||value==='dark'?value:'system'}
export function PreferencesProvider({children}:{children:ReactNode}){
 const [language,updateLanguage]=useState<Language>(readLanguage);
 const [appearance,updateAppearance]=useState<Appearance>(readAppearance);
 const [systemDark,setSystemDark]=useState(()=>window.matchMedia('(prefers-color-scheme: dark)').matches);
 useEffect(()=>{const query=window.matchMedia('(prefers-color-scheme: dark)');const change=(event:MediaQueryListEvent)=>setSystemDark(event.matches);query.addEventListener('change',change);return()=>query.removeEventListener('change',change)},[]);
 useEffect(()=>{const sync=(event:StorageEvent)=>{if(event.key==='datascope-language')updateLanguage(readLanguage());if(event.key==='datascope-theme')updateAppearance(readAppearance())};window.addEventListener('storage',sync);return()=>window.removeEventListener('storage',sync)},[]);
 const dark=appearance==='dark'||(appearance==='system'&&systemDark);
 useEffect(()=>{document.documentElement.dataset.theme=dark?'dark':'light';document.documentElement.lang=language;document.title=language==='en'?'DataScope Admin':'DataScope 管理平台'},[dark,language]);
 useEffect(()=>{ConfigProvider.config({holderRender:children=><ConfigProvider locale={language==='en'?enUS:zhCN} theme={buildTheme(dark)}>{children}</ConfigProvider>})},[language,dark]);
 const setLanguage=(value:Language)=>{localStorage.setItem('datascope-language',value);updateLanguage(value)};
 const setAppearance=(value:Appearance)=>{localStorage.setItem('datascope-theme',value);updateAppearance(value)};
 const t=useMemo<Translate>(()=>(message,params)=>{let value=language==='en'?(english[message]??message):message;for(const [key,replacement]of Object.entries(params??{}))value=value.split('{'+key+'}').join(String(replacement));return value},[language]);
 return <PreferencesContext.Provider value={{language,appearance,dark,setLanguage,setAppearance,t}}><ConfigProvider locale={language==='en'?enUS:zhCN} theme={buildTheme(dark)}>{children}</ConfigProvider></PreferencesContext.Provider>;
}

// Apple-style frosted glass tokens; the translucent layers let the body gradient show through.
function buildTheme(dark: boolean): ThemeConfig {
  const glass = dark ? 'rgba(36, 36, 40, 0.55)' : 'rgba(255, 255, 255, 0.6)';
  const glassStrong = dark ? 'rgba(28, 28, 32, 0.72)' : 'rgba(255, 255, 255, 0.74)';
  return {
    algorithm: dark ? theme.darkAlgorithm : theme.defaultAlgorithm,
    token: {
      fontFamily: '-apple-system, BlinkMacSystemFont, "SF Pro Text", "PingFang SC", "Helvetica Neue", Arial, sans-serif',
      fontSize: 13,
      controlHeight: 30,
      controlHeightSM: 24,
      borderRadius: 8,
      borderRadiusLG: 12,
      colorPrimary: '#0071e3',
      colorBgContainer: glass,
      colorBgElevated: glassStrong,
      colorBgLayout: 'transparent',
      colorBorderSecondary: dark ? 'rgba(255, 255, 255, 0.08)' : 'rgba(255, 255, 255, 0.85)',
    },
    components: {
      Layout: { headerBg: glassStrong, headerHeight: 44 },
      Menu: { horizontalLineHeight: '42px' },
      Card: { headerHeight: 40, headerHeightSM: 36, paddingLG: 14 },
      Table: {
        colorBgContainer: 'transparent',
        headerBg: dark ? '#262629' : '#f5f5f7',
        // Set explicitly: antd derives these from the translucent container, which renders near-black on hover.
        rowHoverBg: dark ? '#2c2c30' : '#f0f6ff',
        bodySortBg: dark ? '#222225' : '#fafafc',
        headerSortHoverBg: dark ? '#303034' : '#ebebf0',
        cellPaddingBlock: 6,
        cellPaddingInline: 10,
        cellPaddingBlockSM: 4,
        cellPaddingInlineSM: 8,
      },
    },
  };
}
