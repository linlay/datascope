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
 const [touch,setTouch]=useState(()=>window.matchMedia('(max-width: 760px), (pointer: coarse)').matches);
 useEffect(()=>{const query=window.matchMedia('(max-width: 760px), (pointer: coarse)');const change=()=>setTouch(query.matches);query.addEventListener('change',change);return()=>query.removeEventListener('change',change)},[]);
 useEffect(()=>{const keyboard=(event:KeyboardEvent)=>{if(event.key==='Tab')document.documentElement.dataset.input='keyboard'};const pointer=()=>{document.documentElement.dataset.input='pointer'};document.addEventListener('keydown',keyboard);document.addEventListener('pointerdown',pointer);return()=>{document.removeEventListener('keydown',keyboard);document.removeEventListener('pointerdown',pointer)}},[]);
 const dark=appearance==='dark'||(appearance==='system'&&systemDark);
 useEffect(()=>{document.documentElement.dataset.theme=dark?'dark':'light';document.documentElement.lang=language;document.title=language==='en'?'DataScope Admin':'DataScope 管理平台'},[dark,language]);
 useEffect(()=>{ConfigProvider.config({holderRender:children=><ConfigProvider locale={language==='en'?enUS:zhCN} theme={buildTheme(dark,touch)}>{children}</ConfigProvider>})},[language,dark,touch]);
 const setLanguage=(value:Language)=>{localStorage.setItem('datascope-language',value);updateLanguage(value)};
 const setAppearance=(value:Appearance)=>{localStorage.setItem('datascope-theme',value);updateAppearance(value)};
 const t=useMemo<Translate>(()=>(message,params)=>{let value=language==='en'?(english[message]??message):message;for(const [key,replacement]of Object.entries(params??{}))value=value.split('{'+key+'}').join(String(replacement));return value},[language]);
 return <PreferencesContext.Provider value={{language,appearance,dark,setLanguage,setAppearance,t}}><ConfigProvider locale={language==='en'?enUS:zhCN} theme={buildTheme(dark,touch)}>{children}</ConfigProvider></PreferencesContext.Provider>;
}

// Shared compact controls and neutral glass surfaces, including imperative dialogs.
function buildTheme(dark: boolean, touch: boolean): ThemeConfig {
 const primary=dark?'#499cff':'#007aff';
 const glass=dark?'rgba(35,41,47,.62)':'rgba(255,255,255,.62)';
 const raised=dark?'rgba(33,40,47,.90)':'rgba(255,255,255,.88)';
 const fieldBorders = {
  colorBorder:'var(--color-border-soft)',
  hoverBorderColor:'var(--field-hover-border, var(--color-border-control))',
  activeBorderColor:'var(--color-primary-border)',
  colorError:'var(--danger)', colorErrorBorderHover:'var(--danger)',
 };
 const textField = {
  ...fieldBorders,
  hoverBg:'var(--field-background, var(--glass-hover))',
  activeBg:'var(--field-background, var(--glass-field))',
  activeShadow:'none', errorActiveShadow:'none', warningActiveShadow:'none',
 };
 return {
  algorithm: dark ? theme.darkAlgorithm : theme.defaultAlgorithm,
  token: {
   fontFamily: '-apple-system, BlinkMacSystemFont, "SF Pro Text", "Segoe UI", "PingFang SC", sans-serif',
   fontSize:13, controlHeight:touch?44:32, controlHeightSM:touch?44:32,
   borderRadius:6, borderRadiusSM:6, borderRadiusLG:12,
   colorPrimary:primary, colorBgContainer:glass, colorBgElevated:raised, colorBgLayout:'transparent',
   colorBorderSecondary:dark?'rgba(210,229,242,.13)':'rgba(255,255,255,.72)',
  },
  components: {
   Layout:{headerBg:glass,headerHeight:52,siderBg:glass},
   Menu:{itemBg:'transparent',darkItemBg:'transparent',darkSubMenuItemBg:'transparent',itemHeight:touch?44:36,itemBorderRadius:6},
   Button:{controlHeight:touch?44:28,controlHeightSM:touch?44:28,paddingInline:8,paddingInlineSM:8,fontWeight:500,primaryShadow:'none',defaultShadow:'none',dangerShadow:'none'},
   Input:{...textField,paddingInline:10},
   InputNumber:textField,
   Select:{
    ...fieldBorders,selectorBg:'var(--glass-field)',optionHeight:touch?44:28,optionPadding:'4px 8px',
    activeOutlineColor:'transparent',colorErrorOutline:'transparent',colorWarningOutline:'transparent',
   },
   DatePicker:{...textField,cellHeight:touch?44:32},
   Form:{itemMarginBottom:12,verticalLabelPadding:'0 0 4px'},
   Card:{headerHeight:44,headerHeightSM:40,paddingLG:16,headerFontSize:15,headerFontSizeSM:15},
   Modal:{padding:16,paddingContentHorizontalLG:16},
   Table:{colorBgContainer:'transparent',headerBg:dark?'rgba(149,173,190,.06)':'rgba(255,255,255,.28)',
    rowHoverBg:dark?'rgba(144,169,187,.14)':'rgba(255,255,255,.8)',bodySortBg:'transparent',
    headerSortHoverBg:dark?'#303b44':'#e9eff4',cellFontSize:13,cellFontSizeSM:13,
    cellPaddingBlock:touch?8:4,cellPaddingInline:10,cellPaddingBlockSM:touch?8:4,cellPaddingInlineSM:10},
  },
 };
}
