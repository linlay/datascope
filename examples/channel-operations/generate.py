#!/usr/bin/env python3
"""Fictional multi-platform operations records. Standard library, fixed seed, no overwrite."""
import argparse
import calendar
import datetime as dt
import json
import math
import os
from pathlib import Path
import random
import sqlite3

ROOT = Path(__file__).resolve().parents[2]
SNAPSHOT = dt.date(2026, 11, 13)
TABLE_LABELS = dict(zip('teams brands users platforms channel_accounts account_grants campaigns campaign_accounts content_items content_versions publications publication_events metric_definitions ingestion_batches account_daily_metrics publication_daily_metrics publication_metric_snapshots ad_sets ad_daily_metrics tracked_links landing_daily_metrics conversion_events comments moderation_events schema_comments'.split(), '运营团队 品牌 业务成员 模拟平台 渠道账号 账号授权 运营活动 活动参与账号 内容选题 平台内容版本 发布任务 发布状态历史 指标口径 采集批次 账号每日指标 内容每日指标 内容累计快照 推广单元 推广日报 跟踪链接 落地页日报 转化事件 评论 评论处置历史 中文数据字典'.split()))
LABELS = {'id':'编号','name':'名称','kind':'类型','login_no':'业务登录编号','team_id':'运营团队编号','role':'业务角色','active':'是否有效','platform_id':'平台编号','brand_id':'品牌编号','content_format':'平台内容形式','status':'业务状态','timezone':'业务时区','user_id':'成员编号','account_id':'渠道账号编号','access':'授权级别','valid_from':'授权起始日（含）','valid_to':'授权结束日（不含）','start_date':'活动开始日','end_date':'活动结束日','objective':'活动目标','campaign_id':'运营活动编号','topic':'选题名称','editor_id':'负责编辑','partner_id':'外部协作者','brief':'内容简报','version':'并发变更版本','content_id':'选题编号','revision':'版本或数据修订号','title':'平台标题','body':'内容正文（虚构）','asset_uri':'虚构素材标识，不连接外部资源','state':'流程状态','author_id':'作者编号','reviewed_by':'审核人','review_note':'审核意见','version_id':'内容版本编号','scheduled_at':'北京时间计划发布时间','published_at':'北京时间实际发布时间','external_post_id':'模拟平台发布编号','failure_reason':'发布失败原因','publication_id':'发布任务编号','actor_id':'操作人编号','from_status':'原发布状态','to_status':'新发布状态','occurred_at':'北京时间事件时间','note':'说明','code':'指标代码','label':'中文指标名称','unit':'单位','aggregation':'聚合方式','definition':'口径及不可比限制','business_date':'业务日期','captured_at':'北京时间采集时间','batch_id':'采集批次','metric_date':'指标业务日期','new_followers':'当日新增关注','unfollows':'当日取消关注','followers_end':'当日期末关注数（存量）','availability':'是否已采集；missing不等于零','views':'当日平台阅读或播放次数','likes':'当日点赞次数','comment_count':'当日评论次数（含未采集评论正文）','shares':'当日分享次数','organic_views':'自然阅读播放；不可拆分时为空','paid_views':'推广阅读播放；不可拆分时为空','cumulative_views':'累计阅读播放快照，禁止跨日求和','source_note':'采集和修订说明','budget_cents':'推广预算（分）','currency':'币种','ad_set_id':'推广单元编号','spend_cents':'当日消耗（分）','impressions':'当日展示次数','clicks':'当日点击次数','platform_reported_conversions':'平台自有口径转化，不能与站内归因相加','tracking_code':'匿名跟踪码','landing_path':'虚构站内落地路径','link_id':'跟踪链接编号','page_views':'当日页面浏览次数','sessions':'当日会话数（不等于跨平台去重人数）','event_key':'采集去重事件键','event_type':'报名或留资','last_click_at':'最后一次可归因点击时间','attribution':'最后点击七天或未知','quality':'有效、重复线索或无效线索','parent_id':'上级评论编号','author_alias':'虚构匿名昵称','created_at':'北京时间评论时间','reply_text':'本地模拟回复文本，不发送至平台','handled_by':'最后处理成员','comment_id':'评论编号','action':'处置操作','reason':'操作原因','table_name':'物理表名','column_name':'物理字段名','comment_zh':'中文说明'}


def stamp(day, hour=10):
    return f'{day}T{hour:02d}:00:00+08:00'


def validate(db):
    checks = {
        'foreign_keys': 'SELECT COUNT(*) FROM pragma_foreign_key_check',
        'brand_consistency': 'SELECT COUNT(*) FROM publications p JOIN channel_accounts a ON a.id=p.account_id JOIN content_versions v ON v.id=p.version_id JOIN content_items c ON c.id=v.content_id JOIN campaigns x ON x.id=c.campaign_id WHERE x.brand_id<>a.brand_id OR a.platform_id<>v.platform_id OR NOT EXISTS(SELECT 1 FROM campaign_accounts ca WHERE ca.campaign_id=x.id AND ca.account_id=a.id)',
        'approved_publication': "SELECT COUNT(*) FROM publications p JOIN content_versions v ON v.id=p.version_id WHERE p.status<>'draft' AND v.state<>'approved'",
        'campaign_dates': 'SELECT COUNT(*) FROM publications p JOIN content_versions v ON v.id=p.version_id JOIN content_items c ON c.id=v.content_id JOIN campaigns x ON x.id=c.campaign_id WHERE substr(COALESCE(p.published_at,p.scheduled_at),1,10) NOT BETWEEN x.start_date AND x.end_date',
        'last_publication_state': 'SELECT COUNT(*) FROM publications p WHERE NOT EXISTS(SELECT 1 FROM publication_events e WHERE e.id=(SELECT MAX(id) FROM publication_events WHERE publication_id=p.id) AND e.to_status=p.status)',
        'event_continuity': 'SELECT COUNT(*) FROM (SELECT from_status,LAG(to_status) OVER(PARTITION BY publication_id ORDER BY id) previous FROM publication_events) WHERE previous IS NOT NULL AND from_status<>previous',
        'daily_batch_platform': 'SELECT COUNT(*) FROM publication_daily_metrics m JOIN publications p ON p.id=m.publication_id JOIN channel_accounts a ON a.id=p.account_id JOIN ingestion_batches b ON b.id=m.batch_id WHERE b.platform_id<>a.platform_id OR b.business_date<>m.metric_date',
        'metrics_dates': "SELECT COUNT(*) FROM publication_daily_metrics m JOIN publications p ON p.id=m.publication_id WHERE p.status NOT IN('published','removed') OR m.metric_date<substr(p.published_at,1,10) OR m.metric_date>'2026-11-13'",
        'account_batch': 'SELECT COUNT(*) FROM account_daily_metrics m JOIN channel_accounts a ON a.id=m.account_id JOIN ingestion_batches b ON b.id=m.batch_id WHERE a.platform_id<>b.platform_id OR m.metric_date<>b.business_date',
        'snapshot_platform': 'SELECT COUNT(*) FROM publication_metric_snapshots m JOIN publications p ON p.id=m.publication_id JOIN channel_accounts a ON a.id=p.account_id JOIN ingestion_batches b ON b.id=m.batch_id WHERE a.platform_id<>b.platform_id OR substr(m.captured_at,1,10)<b.business_date',
        'snapshot_matches_daily': 'SELECT COUNT(*) FROM publication_daily_metrics m WHERE m.availability=\'complete\' AND NOT EXISTS(SELECT 1 FROM publication_metric_snapshots s WHERE s.publication_id=m.publication_id AND s.batch_id=m.batch_id)',
        'ad_campaign': 'SELECT COUNT(*) FROM ad_sets d JOIN publications p ON p.id=d.publication_id JOIN content_versions v ON v.id=p.version_id JOIN content_items c ON c.id=v.content_id WHERE d.campaign_id<>c.campaign_id',
        'ad_budget': 'SELECT COUNT(*) FROM ad_sets d WHERE (SELECT COALESCE(SUM(spend_cents),0) FROM ad_daily_metrics m WHERE m.ad_set_id=d.id)>d.budget_cents',
        'ad_date': 'SELECT COUNT(*) FROM ad_daily_metrics m JOIN ad_sets d ON d.id=m.ad_set_id JOIN publications p ON p.id=d.publication_id WHERE m.metric_date<substr(p.published_at,1,10) OR m.metric_date>\'2026-11-13\'',
        'link_campaign': 'SELECT COUNT(*) FROM tracked_links l JOIN publications p ON p.id=l.publication_id JOIN content_versions v ON v.id=p.version_id JOIN content_items c ON c.id=v.content_id WHERE l.campaign_id<>c.campaign_id',
        'conversion_campaign': 'SELECT COUNT(*) FROM conversion_events e JOIN tracked_links l ON l.id=e.link_id WHERE e.campaign_id<>l.campaign_id',
        'attribution_window': "SELECT COUNT(*) FROM conversion_events WHERE attribution='last_click_7d' AND (julianday(occurred_at)<julianday(last_click_at) OR julianday(occurred_at)-julianday(last_click_at)>7)",
        'comment_parent': 'SELECT COUNT(*) FROM comments c JOIN comments p ON p.id=c.parent_id WHERE c.publication_id<>p.publication_id OR julianday(c.created_at)<julianday(p.created_at)',
        'comment_date': 'SELECT COUNT(*) FROM comments c JOIN publications p ON p.id=c.publication_id WHERE julianday(c.created_at)<julianday(p.published_at) OR substr(c.created_at,1,10)>\'2026-11-13\'',
        'moderation_date': 'SELECT COUNT(*) FROM moderation_events e JOIN comments c ON c.id=e.comment_id WHERE julianday(e.occurred_at)<julianday(c.created_at)',
        'reviewer': "SELECT COUNT(*) FROM content_versions v JOIN users u ON u.id=v.reviewed_by WHERE u.role NOT IN('LEAD','ADMIN') OR u.active<>1",
        'conversion_landing': 'SELECT COUNT(*) FROM conversion_events e JOIN tracked_links l ON l.id=e.link_id WHERE NOT EXISTS(SELECT 1 FROM landing_daily_metrics m WHERE m.link_id=l.id AND m.metric_date=substr(e.last_click_at,1,10))',
    }
    for name, query in checks.items():
        n = db.execute(query).fetchone()[0]
        if n:
            raise ValueError(f'{name}: {n} violations')
    if db.execute('PRAGMA integrity_check').fetchone()[0] != 'ok':
        raise ValueError('SQLite integrity validation failed')
    return len(checks)


def generate(path, seed=20261113):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    fd = os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    os.close(fd)
    db = None
    complete = False
    try:
        db = sqlite3.connect(path)
        db.executescript(Path(__file__).with_name('schema.sqlite.sql').read_text(encoding='utf-8'))
        rng = random.Random(seed)
        def put(table, *args):
            db.execute(f'INSERT INTO {table} VALUES ({",".join("?" for _ in args)})', args)
        for i,name in enumerate(['品牌内容一组','品牌内容二组','青禾创作工作室'],1): put('teams',i,name,'agency' if i==3 else 'internal')
        for i,name in enumerate(['原野生活','山岚营地'],1): put('brands',i,name)
        roles=['ADMIN','LEAD','ANALYST','EDITOR','OPERATOR','PARTNER','LEAD','EDITOR','ANALYST','EDITOR','EDITOR','EDITOR','EDITOR','OPERATOR','OPERATOR','OPERATOR','OPERATOR','OPERATOR','PARTNER','OPERATOR']
        names=['陈知远','林若溪','王思涵','李嘉宁','张宇航','刘清扬','赵景行','黄知夏','周文博','吴以宁','徐安然','孙沐辰','胡书瑶','朱锦程','高予安','郭承泽','何允和','罗舒雅','郑嘉言','梁亦航']
        for i,(role,name) in enumerate(zip(roles,names),1):
            team=3 if role=='PARTNER' else 2 if i in (7,8,9,12,13,15,16,18) else 1
            put('users',i,f'OPS{i:03d}',name,team,role,int(i!=20))
        for i,(name,fmt) in enumerate([('短视频平台A','video'),('图文社区B','image_text'),('订阅内容平台C','article'),('视频社区D','video')],1):
            put('platforms',i,name,fmt)
            for code,label,agg,definition in [('views','阅读或播放次数','daily_additive','平台自身定义的阅读或播放次数；平台间不可视为相同触达口径'),('followers_end','期末关注数','cumulative_snapshot','时点存量，禁止跨日求和'),('cumulative_views','累计阅读播放','cumulative_snapshot','截至采集时刻的累计值；可能因反作弊修订下降，禁止跨日求和')]:
                put('metric_definitions',i,code,label,'次' if code!='followers_end' else '关注关系',agg,definition)
        for i in range(1,13):
            brand=1 if i<=6 else 2
            put('channel_accounts',i,(i-1)%4+1,brand,brand,['原野生活','山岚营地'][brand-1]+'·'+['品牌主号','露营指南','城市活动','装备研究','山野影像','周末计划'][(i-1)%6],'suspended' if i==12 else 'active','Asia/Shanghai')
            for uid,access in [(2 if brand==1 else 7,'OPERATE'),(3 if brand==1 else 9,'READ'),(5 if i<=3 else 14 if i<=6 else 15 if i<=9 else 16,'OPERATE')]:
                put('account_grants',uid,i,access,'2026-05-01',None)
        put('account_grants',17,1,'READ','2026-05-01',None)
        put('account_grants',18,7,'OPERATE','2026-05-01','2026-10-01')
        campaign_month={}
        for i in range(1,31):
            local=(i-1)%15;brand=(i-1)//15+1;month=11 if local==14 else 5+local%6
            campaign_month[i]=month
            title=['周末轻露营','山野清洁日','城市徒步季','新手装备课堂','营地体验官','亲子自然课','秋日山野影像','社群共创计划','轻装出行指南','营地开放日','雨天露营攻略','自然观察挑战','无痕山野行动','年度内容共创','冬季活动预告'][local]
            put('campaigns',i,brand,title,f'2026-{month:02d}-01',f'2026-{month:02d}-{calendar.monthrange(2026,month)[1]}',['awareness','registration','lead'][local%3])
            for account in range(1 if brand==1 else 7,7 if brand==1 else 13): put('campaign_accounts',i,account)
        for i in range(1,601):
            brand=(i-1)//300+1;local=(i-1)%300;campaign=(brand-1)*15+local//20+1
            editor=([4,10,11] if brand==1 else [8,12,13])[local%3]
            partner=(6 if brand==1 else 19) if i%10==0 else None
            topic=['第一次露营如何选址','下雨天的营地收纳','周末徒步路线准备','轻量化装备清单','带孩子认识植物','无痕山野行动记录','城市近郊营地探访','营地咖啡与早餐','秋冬保暖分层','活动报名答疑'][local%10]
            put('content_items',i,campaign,brand,topic+f'·第{local//10+1}期',editor,partner,'面向新手的实用内容；避免夸大效果，素材全部虚构。',1)
            for platform in range(1,5):
                vid=(i-1)*4+platform
                state=['draft','submitted','rejected','approved'][vid%4] if i%7==0 else 'approved'
                reviewed=(2 if brand==1 else 7) if state in ('approved','rejected') else None
                author=partner if partner and platform==2 else editor
                put('content_versions',vid,i,platform,1,topic+['｜一分钟看懂','：图文清单','：完整操作指南','｜实地演示'][platform-1],topic+'。先检查天气、路线和装备，再安排活动；以下为虚构运营示例文案。',f'asset://fictional/{i:04d}/{platform}',state,author,reviewed,'审核通过' if state=='approved' else '需补充来源说明' if state=='rejected' else None)
        batches={};day=dt.date(2026,5,1);bid=0
        while day<=SNAPSHOT:
            for platform in range(1,5):
                bid+=1;partial=bid%19==0
                put('ingestion_batches',bid,platform,day.isoformat(),stamp(day,23),'partial' if partial else 'complete','部分账号晚到，保留缺失标记' if partial else '模拟平台日报采集')
                batches[(platform,day)]=bid
            day+=dt.timedelta(days=1)
        for account in range(1,13):
            followers=3000+account*1871
            day=dt.date(2026,5,1)
            while day<=SNAPSHOT:
                new=rng.randint(4,100);lost=rng.randint(0,25);followers+=new-lost
                missing=(account+day.day)%37==0
                put('account_daily_metrics',account,day.isoformat(),batches[((account-1)%4+1,day)],None if missing else new,None if missing else lost,None if missing else followers,'missing' if missing else 'complete')
                day+=dt.timedelta(days=1)
        event_id=snapshot_id=ad_id=conversion_id=comment_id=moderation_id=0
        for i in range(1,1601):
            content=(i-1)%600+1;brand=(content-1)//300+1;local=(content-1)%300
            account=(brand-1)*6+(local+(i-1)//600)%6+1;platform=(account-1)%4+1;version=(content-1)*4+platform;campaign=(brand-1)*15+local//20+1
            month=campaign_month[campaign];day=dt.date(2026,month,1+(local%20+i//600)%min(20,12 if month==11 else 28))
            state='draft' if i%20==0 else 'failed' if i%20==1 else 'removed' if i%20==2 else 'published'
            if month==11 and i%4==0 and state=='published': state='scheduled';day=SNAPSHOT+dt.timedelta(days=i%7+1)
            operator=5 if account<=3 else 14 if account<=6 else 15 if account<=9 else 16
            if account==12 and state not in ('published','removed'): state='draft'
            if state!='draft': db.execute("UPDATE content_versions SET state='approved',reviewed_by=?,review_note='审核通过' WHERE id=?",(2 if brand==1 else 7,version))
            published=stamp(day,10) if state in ('published','removed') else None
            put('publications',i,version,account,state,stamp(day,9) if state!='draft' else None,published,f'SIM-{platform}-{i:06d}' if published else None,'模拟素材校验失败' if state=='failed' else None,1)
            events=[('draft',None,stamp(min(day,SNAPSHOT),8))]
            if state!='draft':events.append(('scheduled','draft',stamp(SNAPSHOT if state=='scheduled' else day,9)))
            if state in ('published','removed','failed'): events.append(('published' if published else 'failed','scheduled',stamp(day,10)))
            if state=='removed':events.append(('removed','published',stamp(min(day+dt.timedelta(days=5),SNAPSHOT),18)))
            for to,old,when in events:
                event_id+=1;put('publication_events',event_id,i,operator,old,to,when,'本地模拟状态流转，不调用真实平台')
            if not published: continue
            paid=i%7==0
            if paid:
                ad_id+=1;put('ad_sets',ad_id,i,campaign,f'内容{i:04d}小额推广',150000,'CNY','paused' if i%3==0 else 'closed')
            put('tracked_links',i,i,campaign,f'UTM-{i:06d}',f'/events/{campaign}/signup')
            cumulative=0;potential=round(math.exp(rng.gauss(7.5,1.1)));seen_comments=[]
            for age in range(30):
                metric_day=day+dt.timedelta(days=age)
                if metric_day>SNAPSHOT: break
                if state=='removed' and age>5: break
                views=max(0,round(potential*math.exp(-age/5)*rng.uniform(.5,1.5)))
                if i%89==0 and age==8: views*=12
                missing=(i+age)%53==0
                late=(i+age)%41==0 and metric_day<SNAPSHOT
                organic=None if platform==3 else views*7//10 if paid else views
                paid_views=None if organic is None else views-organic
                batch=batches[(platform,metric_day)]
                likes=round(views*rng.uniform(.01,.08));com=max(20+i%6 if age==0 else 0,round(views*rng.uniform(.001,.012)));shares=round(views*rng.uniform(.001,.008))
                put('publication_daily_metrics',i,metric_day.isoformat(),batch,None if missing else views,None if missing else likes,None if missing else com,None if missing else shares,None if missing else organic,None if missing else paid_views,'missing' if missing else 'complete',2 if late else 1)
                # Cumulative source counters include days whose daily report is missing.
                cumulative+=views
                if late:
                    snapshot_id+=1;put('publication_metric_snapshots',snapshot_id,i,batch,stamp(metric_day,22),cumulative+150,1,'原始累计值，后续反作弊修订；不直接差分为每日指标')
                snapshot_id+=1;capture=metric_day+dt.timedelta(days=1) if late else metric_day
                put('publication_metric_snapshots',snapshot_id,i,batch,stamp(capture,10 if late else 23),cumulative,2 if late else 1,'修订回补；累计值可能下降' if late else '原始累计快照')
                if paid and age<10:
                    spend=rng.randint(1000,6500);impressions=max(views*2,100);clicks=rng.randint(0,max(1,impressions//20))
                    put('ad_daily_metrics',ad_id,metric_day.isoformat(),spend,impressions,clicks,clicks//25)
                sessions=max(0,views//45);put('landing_daily_metrics',i,metric_day.isoformat(),sessions+rng.randint(0,20),sessions)
                for j in range(min(3,sessions//20)):
                    conversion_id+=1
                    occurred=min(metric_day+dt.timedelta(days=rng.randint(0,6)),SNAPSHOT)
                    quality='duplicate' if conversion_id%19==0 else 'invalid' if conversion_id%23==0 else 'valid'
                    put('conversion_events',conversion_id,f'EVT-{conversion_id:08d}',campaign,i,'registration' if campaign%2 else 'lead',stamp(occurred,16),stamp(metric_day,12),'last_click_7d',quality)
                if age==0:
                    for j in range(20+(i%6)):
                        comment_id+=1;created=stamp(day,11);state_c=['pending','visible','hidden','replied'][comment_id%4]
                        parent=seen_comments[-1] if seen_comments and j%7==0 else None
                        body=['新手可以参加吗？','请问需要提前报名吗？','这份清单很实用，已收藏。','附近还有哪些适合周末的路线？','雨天活动会延期吗？','这款装备的适用场景是什么？'][j%6]
                        reply='感谢关注，请以活动页的报名条件和天气通知为准。' if state_c=='replied' else None
                        put('comments',comment_id,i,parent,f'山野同行{comment_id:05d}',body,created,state_c,reply,operator if state_c!='pending' else None)
                        seen_comments.append(comment_id)
                        if state_c!='pending':
                            moderation_id+=1;put('moderation_events',moderation_id,comment_id,operator,{'visible':'approve','hidden':'hide','replied':'reply'}[state_c],stamp(day,13),'模拟人工审核；回复仅保存在本地')
        # Anonymous conversions without a reliable link remain explicitly unassigned.
        for campaign in range(1,31):
            for j in range(4):
                conversion_id+=1;day=dt.date(2026,campaign_month[campaign],j+2)
                put('conversion_events',conversion_id,f'EVT-{conversion_id:08d}',campaign,None,'lead',stamp(day,16),None,'unknown','valid')
        for table,label in TABLE_LABELS.items():
            put('schema_comments',table,'',label)
            for col in db.execute('PRAGMA table_info('+table+')').fetchall(): put('schema_comments',table,col[1],LABELS[col[1]])
        checks=validate(db);db.execute('ANALYZE');db.commit();complete=True
        return {'database':str(path),'seed':seed,'as_of':str(SNAPSHOT),'bytes':path.stat().st_size,'invariant_checks':checks,'rows':{t:db.execute('SELECT COUNT(*) FROM '+t).fetchone()[0] for t in TABLE_LABELS}}
    finally:
        if db is not None: db.close()
        if not complete:path.unlink(missing_ok=True)


if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--db',default=str(ROOT/'data/channel_operations_demo.sqlite3'))
    parser.add_argument('--seed',type=int,default=20261113)
    args=parser.parse_args()
    print(json.dumps(generate(args.db,args.seed),ensure_ascii=False,indent=2))
