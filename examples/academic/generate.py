#!/usr/bin/env python3
"""Deterministic fictional college records; Python standard library only; never overwrites."""
import argparse
import datetime as dt
import json
import os
from pathlib import Path
import random
import sqlite3

ROOT = Path(__file__).resolve().parents[2]
SCHOOLS = [
    ('信息工程学院', ['计算机科学与技术', '软件工程'], ['程序设计基础','离散数学','计算机导论','高等数学','数据结构','计算机组成原理','数据库原理','概率论与数理统计','操作系统','计算机网络','软件工程','算法设计','分布式系统','软件测试','信息安全','毕业设计方法','Python数据分析','网页设计','开源软件实践','人工智能导论']),
    ('经济管理学院', ['工商管理', '会计学'], ['管理学原理','微观经济学','基础会计','经济数学','组织行为学','宏观经济学','中级财务会计','统计学','运营管理','财务管理','管理会计','经济法','战略管理','审计学','企业风险管理','商业研究方法','创新创业基础','消费者行为','商务沟通','数字营销']),
    ('外国语学院', ['英语', '商务英语'], ['综合英语一','英语语音','英语阅读一','英语听力一','综合英语二','英语语法','英语阅读二','英语听力二','英汉翻译','英语写作','语言学概论','跨文化交际','高级英语','学术英语','口译基础','外国文学选读','英语演讲','商务谈判','国际文化交流','英文报刊阅读']),
    ('智能制造学院', ['机械设计制造及其自动化', '自动化'], ['工程制图','工程力学','电路基础','大学物理','机械原理','模拟电子技术','数字电子技术','工程材料','机械设计','自动控制原理','传感器原理','制造技术基础','机电系统设计','工业机器人','先进制造技术','控制系统仿真','三维建模','智能工厂导论','工程项目管理','工业设计基础']),
    ('人文与艺术学院', ['汉语言文学', '视觉传达设计'], ['现代汉语','文学概论','设计素描','艺术概论','古代汉语','中国古代文学','设计色彩','传播学概论','中国现当代文学','外国文学','字体设计','版式设计','文学批评','文化研究方法','品牌设计','毕业创作方法','公共艺术欣赏','摄影基础','创意写作','非遗文化导论']),
]
TABLE_LABELS = dict(zip('schools majors users classes teachers counselor_assignments students student_contacts terms courses prerequisites rooms sections section_teachers section_meetings prior_credits enrollments assessments grades grade_releases attendance exams schema_comments'.split(), '学院 专业 业务身份 行政班 教师 辅导员任职 学生 学生私密联系方式 学期 课程 先修课 教室 教学班 授课团队 每周课表 历史学分认定 选课记录 考核项目 成绩 成绩发布 考勤 考试安排 中文数据字典'.split()))
COL_LABELS = {'id':'主键编号','code':'业务代码','name':'名称','school_id':'所属学院编号','major_id':'专业编号','login_no':'业务登录编号','role':'业务角色','active':'账号是否有效','admission_year':'入学年份','title':'教师职称','counselor_id':'辅导员身份编号','class_id':'行政班编号','valid_from':'任职生效日（含）','valid_to':'任职结束日（不含，空表示持续）','student_no':'学号','status':'业务状态','student_id':'学生身份编号','email':'虚构电子邮箱','mobile':'虚构联系电话','emergency_contact':'紧急联系人（业务查询不开放）','start_date':'开课日期（周一）','end_date':'学期结束日','enroll_from':'选课开始日','add_drop_until':'选退课截止日（含）','max_credits':'学期选课学分上限','credits':'课程学分','kind':'课程或考核类型','room_type':'教室类型','course_id':'课程编号','required_course_id':'先修课程编号','building':'教学楼','room_no':'教室门牌','capacity':'人数上限','term_id':'学期编号','section_no':'教学班编码','audience_year':'建议修读年级','version':'教学班变更版本号','section_id':'教学班编号','teacher_id':'授课教师身份编号','is_lead':'是否主讲教师','room_id':'教室编号','weekday':'星期几（1为周一）','period_from':'开始节次（含）','period_to':'结束节次（含）','week_from':'起始教学周（含）','week_to':'结束教学周（含）','score':'百分制成绩（缺考为0，待录入为空）','recognized_on':'学分认定日期','source':'学分来源','enrolled_on':'选课日期','dropped_on':'退课日期','weight':'成绩权重（整数百分比）','assessment_date':'考核日期','enrollment_id':'选课记录编号','assessment_id':'考核项目编号','graded_by':'成绩录入教师','graded_on':'成绩录入日期','released_by':'成绩发布教师','released_at':'成绩发布时间（中国时区）','meeting_id':'课表记录编号','attendance_date':'考勤日期','recorded_by':'考勤登记教师','exam_date':'考试日期','invigilator_id':'监考教师','table_name':'物理表名','column_name':'物理字段名','comment_zh':'中文说明'}


TABLE_LABELS['calendar_days'] = '示例教学日历'
COL_LABELS.update(calendar_date='校历日期',teaching_week='教学周序号',teaching_weekday='校历对应星期',is_teaching_day='是否安排常规教学',note='校历说明（虚构）')


def person(i):
    surnames = '陈 林 王 李 张 刘 赵 黄 周 吴 徐 孙 胡 朱 高 郭 何 罗 郑 梁 谢 宋 唐 许 韩 冯 邓 曹 彭 曾 田 董 袁 潘 于 蒋 蔡 余 杜 叶 程 苏 魏 吕 丁 任 沈 姚 卢 姜'.split()
    given = '明远 思涵 嘉宁 子轩 雨桐 宇航 佳怡 文博 诗涵 浩然 欣悦 俊杰 晓彤 梓睿 若琳 博文 梦琪 逸辰 静怡 皓宇 婉清 泽宇 语嫣 景行 安然 一诺 沐辰 书瑶 星宇 昕妍 奕帆 予安'.split()
    return surnames[i % len(surnames)] + given[i // len(surnames) % len(given)]


def validate(db):
    checks = {
        'foreign_keys': 'SELECT COUNT(*) FROM pragma_foreign_key_check',
        'assessment_weights': 'SELECT COUNT(*) FROM (SELECT section_id FROM assessments GROUP BY section_id HAVING SUM(weight)<>100)',
        'grade_section': 'SELECT COUNT(*) FROM grades g JOIN enrollments e ON e.id=g.enrollment_id JOIN assessments a ON a.id=g.assessment_id WHERE e.section_id<>a.section_id',
        'grade_author': 'SELECT COUNT(*) FROM grades g JOIN assessments a ON a.id=g.assessment_id WHERE g.graded_by IS NOT NULL AND NOT EXISTS(SELECT 1 FROM section_teachers t WHERE t.section_id=a.section_id AND t.teacher_id=g.graded_by)',
        'released_incomplete': "SELECT COUNT(*) FROM grade_releases r JOIN assessments a ON a.id=r.assessment_id WHERE NOT EXISTS(SELECT 1 FROM section_teachers t WHERE t.section_id=a.section_id AND t.teacher_id=r.released_by AND t.is_lead=1) OR EXISTS(SELECT 1 FROM enrollments e WHERE e.section_id=a.section_id AND e.status<>'dropped' AND NOT EXISTS(SELECT 1 FROM grades g WHERE g.assessment_id=a.id AND g.enrollment_id=e.id AND g.status<>'pending'))",
        'capacity': "SELECT COUNT(*) FROM sections s WHERE (SELECT COUNT(*) FROM enrollments e WHERE e.section_id=s.id AND e.status<>'dropped')>s.capacity",
        'cancelled_enrollment': "SELECT COUNT(*) FROM enrollments e JOIN sections s ON s.id=e.section_id WHERE s.status='cancelled' AND e.status<>'dropped'",
        'lead_teacher': "SELECT COUNT(*) FROM sections s WHERE (SELECT COUNT(*) FROM section_teachers t WHERE t.section_id=s.id AND t.is_lead=1)<>1",
        'room_capacity_type': 'SELECT COUNT(*) FROM section_meetings m JOIN sections s ON s.id=m.section_id JOIN courses c ON c.id=s.course_id JOIN rooms r ON r.id=m.room_id WHERE s.capacity>r.capacity OR c.room_type<>r.room_type',
        'room_conflict': "SELECT COUNT(*) FROM section_meetings a JOIN section_meetings b ON a.id<b.id AND a.room_id=b.room_id AND a.weekday=b.weekday AND a.period_from<=b.period_to AND b.period_from<=a.period_to AND a.week_from<=b.week_to AND b.week_from<=a.week_to JOIN sections x ON x.id=a.section_id JOIN sections y ON y.id=b.section_id WHERE x.term_id=y.term_id AND x.status<>'cancelled' AND y.status<>'cancelled'",
        'teacher_conflict': "SELECT COUNT(*) FROM section_meetings a JOIN section_meetings b ON a.id<b.id AND a.weekday=b.weekday AND a.period_from<=b.period_to AND b.period_from<=a.period_to JOIN sections x ON x.id=a.section_id JOIN sections y ON y.id=b.section_id JOIN section_teachers tx ON tx.section_id=x.id JOIN section_teachers ty ON ty.section_id=y.id AND ty.teacher_id=tx.teacher_id WHERE x.term_id=y.term_id AND x.status<>'cancelled' AND y.status<>'cancelled' AND a.week_from<=b.week_to AND b.week_from<=a.week_to",
        'student_conflict': "SELECT COUNT(*) FROM enrollments e JOIN enrollments f ON e.student_id=f.student_id AND e.id<f.id JOIN sections x ON x.id=e.section_id JOIN sections y ON y.id=f.section_id JOIN section_meetings a ON a.section_id=x.id JOIN section_meetings b ON b.section_id=y.id WHERE e.status<>'dropped' AND f.status<>'dropped' AND x.term_id=y.term_id AND a.weekday=b.weekday AND a.period_from<=b.period_to AND b.period_from<=a.period_to AND a.week_from<=b.week_to AND b.week_from<=a.week_to",
        'duplicate_course': "SELECT COUNT(*) FROM (SELECT e.student_id,s.term_id,s.course_id FROM enrollments e JOIN sections s ON s.id=e.section_id WHERE e.status<>'dropped' GROUP BY 1,2,3 HAVING COUNT(*)>1)",
        'credit_limit': "SELECT COUNT(*) FROM (SELECT e.student_id,s.term_id FROM enrollments e JOIN sections s ON s.id=e.section_id JOIN courses c ON c.id=s.course_id JOIN terms t ON t.id=s.term_id WHERE e.status<>'dropped' GROUP BY 1,2 HAVING SUM(c.credits)>MAX(t.max_credits))",
        'attendance_section': 'SELECT COUNT(*) FROM attendance a JOIN enrollments e ON e.id=a.enrollment_id JOIN section_meetings m ON m.id=a.meeting_id WHERE e.section_id<>m.section_id',
        'attendance_calendar': 'SELECT COUNT(*) FROM attendance a JOIN section_meetings m ON m.id=a.meeting_id JOIN sections s ON s.id=m.section_id LEFT JOIN calendar_days c ON c.term_id=s.term_id AND c.calendar_date=a.attendance_date WHERE c.is_teaching_day IS NULL OR c.is_teaching_day<>1',
        'release_date': 'SELECT COUNT(*) FROM grade_releases r JOIN assessments a ON a.id=r.assessment_id WHERE substr(r.released_at,1,10)<a.assessment_date',
        'prerequisites': "SELECT COUNT(*) FROM enrollments e JOIN sections s ON s.id=e.section_id JOIN prerequisites p ON p.course_id=s.course_id WHERE e.status<>'dropped' AND NOT EXISTS(SELECT 1 FROM prior_credits c WHERE c.student_id=e.student_id AND c.course_id=p.required_course_id AND c.recognized_on<=e.enrolled_on) AND NOT EXISTS(SELECT 1 FROM enrollments old JOIN sections os ON os.id=old.section_id JOIN assessments a ON a.section_id=os.id JOIN grades g ON g.enrollment_id=old.id AND g.assessment_id=a.id JOIN grade_releases r ON r.assessment_id=a.id WHERE old.student_id=e.student_id AND old.status='completed' AND os.course_id=p.required_course_id AND substr(r.released_at,1,10)<=e.enrolled_on GROUP BY old.id HAVING SUM(a.weight)=100 AND SUM(g.score*a.weight)>=6000)",
        'attendance_dates': "SELECT COUNT(*) FROM attendance a JOIN enrollments e ON e.id=a.enrollment_id JOIN section_meetings m ON m.id=a.meeting_id JOIN sections s ON s.id=m.section_id JOIN terms t ON t.id=s.term_id WHERE CAST(strftime('%w',a.attendance_date) AS INTEGER)<>m.weekday%7 OR a.attendance_date<date(t.start_date,'+'||((m.week_from-1)*7)||' days') OR a.attendance_date>date(t.start_date,'+'||(m.week_to*7-1)||' days') OR a.attendance_date<e.enrolled_on OR (e.dropped_on IS NOT NULL AND a.attendance_date>=e.dropped_on)",
        'exam_capacity': "SELECT COUNT(*) FROM exams x JOIN rooms r ON r.id=x.room_id WHERE (SELECT COUNT(*) FROM enrollments e WHERE e.section_id=x.section_id AND e.status<>'dropped')>r.capacity",
        'exam_room_conflict': 'SELECT COUNT(*) FROM exams x JOIN exams y ON x.id<y.id AND x.room_id=y.room_id AND x.exam_date=y.exam_date AND x.period_from<=y.period_to AND y.period_from<=x.period_to',
        'exam_student_conflict': "SELECT COUNT(*) FROM exams x JOIN exams y ON x.id<y.id AND x.exam_date=y.exam_date AND x.period_from<=y.period_to AND y.period_from<=x.period_to JOIN enrollments a ON a.section_id=x.section_id JOIN enrollments b ON b.section_id=y.section_id AND a.student_id=b.student_id WHERE a.status<>'dropped' AND b.status<>'dropped'",
    }
    for name, sql in checks.items():
        count = db.execute(sql).fetchone()[0]
        if count:
            raise ValueError(f'{name}: {count} violations')
    if db.execute('PRAGMA integrity_check').fetchone()[0] != 'ok':
        raise ValueError('SQLite integrity validation failed')
    return len(checks)


def generate(path, seed=20261113, as_of='2026-11-13'):
    snapshot = dt.date.fromisoformat(as_of)
    if not dt.date(2026, 9, 2) <= snapshot <= dt.date(2026, 12, 27):
        raise ValueError('as-of must be between 2026-09-02 and 2026-12-27')
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
        put('users', 1, 'ACA001', '教务管理员', 'REGISTRAR', None, 1)
        for school, (name, majors, names) in enumerate(SCHOOLS, 1):
            put('schools', school, f'SCH{school:02d}', name)
            for j, major in enumerate(majors):
                mid = (school-1)*2+j+1
                put('majors', mid, school, f'M{mid:02d}', major)
            for j, course in enumerate(names):
                cid = (school-1)*20+j+1
                lab = school == 1 and j in (0,4,6,8,12,16,17,18) or school == 4 and j in (15,16)
                put('courses', cid, school, f'C{cid:03d}', course, 3, 'required' if j<16 else 'elective', 'computer_lab' if lab else 'lecture')
                if 4 <= j < 16:
                    put('prerequisites', cid, cid-4)
        put('users', 2, 'ACA002', '信息学院教务员', 'SCHOOL_ADMIN', 1, 1)
        for i in range(80):
            uid = 1001+i
            put('users', uid, f'T{uid:04d}', person(i), 'TEACHER', i//16+1, int(i!=79))
            put('teachers', uid, ['讲师','副教授','教授','讲师'][i%4])
        for i in range(10):
            put('users', 2001+i, f'F{2001+i:04d}', person(80+i), 'COUNSELOR', i//2+1, 1)
        for mid in range(1, 11):
            for cohort in range(4):
                cid = (mid-1)*4+cohort+1
                year = 2023+cohort
                put('classes', cid, mid, f'{SCHOOLS[(mid-1)//2][1][(mid-1)%2]}{year}级1班', year)
                put('counselor_assignments', cid, 2000+mid, cid, '2023-09-01', '2026-10-01' if cid==4 else None)
        put('counselor_assignments', 41, 2002, 4, '2026-10-01', None)
        for i in range(1000):
            uid, cid = 10001+i, i//25+1
            school = (cid-1)//8+1
            put('users', uid, f'202{3+(cid-1)%4}{cid:02d}{i%25+1:03d}', person(90+i), 'STUDENT', school, 1)
            put('students', uid, f'202{3+(cid-1)%4}{cid:02d}{i%25+1:03d}', cid, 'leave' if i in (149,399,649,899) else 'active')
            put('student_contacts', uid, f's{uid}@students.example.invalid', f'1990000{i:04d}', '紧急联系人（虚构）')
        put('terms', 1, '2025–2026学年第二学期', '2026-03-02', '2026-07-05', '2026-02-23', '2026-03-08', 24)
        put('terms', 2, '2026–2027学年第一学期', '2026-09-07', '2027-01-10', '2026-09-01', '2026-09-20', 24)
        for term,start,end in [(1,dt.date(2026,3,2),dt.date(2026,7,5)),(2,dt.date(2026,9,7),dt.date(2027,1,10))]:
            day=start
            while day<=end:
                week=(day-start).days//7+1
                vacation=day.isoformat()=='2026-04-06' or '2026-05-01'<=day.isoformat()<='2026-05-05' or '2026-10-01'<=day.isoformat()<='2026-10-07'
                teaching=week<=16 and day.weekday()<5 and not vacation
                put('calendar_days',term,day.isoformat(),week,day.isoweekday(),int(teaching),'示例停课日' if vacation else '考试或教学准备周' if week>16 else '周末' if day.weekday()>=5 else '常规教学')
                day+=dt.timedelta(days=1)
        for i in range(1, 81):
            put('rooms', i, ['明德楼','知行楼','求实楼','工训楼'][(i-1)//20], f'{1+(i-1)%20//5}{(i-1)%5+1:02d}', 'computer_lab' if i>50 else 'lecture', 40 if i>50 else 60)
        students = {cid: db.execute("SELECT id FROM students WHERE class_id=? AND status='active' ORDER BY id",(cid,)).fetchall() for cid in range(1,41)}
        courses = {r[0]:r for r in db.execute('SELECT id,school_id,room_type FROM courses')}
        teacher_busy, room_busy = set(), set()
        sid = eid = aid = gid = attendance_id = prior_id = 0
        passed = set()
        abilities={10001+i:rng.gauss(77,10) for i in range(1000)}
        # Earlier semesters are represented by explicit imported credit recognitions.
        for cid in range(1,41):
            year = 2026-(2023+(cid-1)%4)
            school = (cid-1)//8+1
            for (student,) in students[cid]:
                for j in range(max(0,year-1)*4):
                    prior_id += 1
                    course = (school-1)*20+j+1
                    put('prior_credits', prior_id, student, course, rng.randint(60,96), '2026-02-20', 'historical_import')
                    passed.add((student,course))

        def section(term, course, year, slot, capacity=32, cancelled=False):
            nonlocal sid, aid
            sid += 1
            school, room_type = courses[course][1:]
            available = [1001+i for i in range((school-1)*16,school*16) if i!=79 and (term,slot,1001+i) not in teacher_busy]
            rooms = [r[0] for r in db.execute('SELECT id FROM rooms WHERE room_type=? AND capacity>=? ORDER BY id',(room_type,capacity)) if (term,slot,r[0]) not in room_busy]
            if not available or not rooms:
                raise ValueError(f'No feasible timetable: term={term}, slot={slot}, school={school}')
            lead, room = available[0], rooms[0]
            put('sections', sid, term, course, f'{2026}{"S" if term==1 else "F"}-{sid:04d}', capacity, 'cancelled' if cancelled else 'closed' if term==1 else 'open', year, 1)
            put('section_teachers', sid, lead, 1)
            if sid%17==0 and len(available)>1:
                put('section_teachers',sid,available[1],0)
                if not cancelled: teacher_busy.add((term,slot,available[1]))
            if not cancelled:
                teacher_busy.add((term,slot,lead));room_busy.add((term,slot,room))
            day, period = slot//2+1, 1 if slot%2==0 else 5
            put('section_meetings',sid,sid,room,day,period,period+2,1,16)
            start = dt.date(2026,3,2) if term==1 else dt.date(2026,9,7)
            exam = start+dt.timedelta(weeks=16,days=day-1)
            if not cancelled:
                put('exams',sid,sid,room,exam.isoformat(),period,period+2,lead)
            assessment_ids = []
            for kind, weight, week, name in [('coursework',30,5,'阶段作业'),('midterm',20,8,'期中考核'),('final',50,17,'期末考试')]:
                aid+=1
                date = start+dt.timedelta(weeks=week-1,days=day-1)
                put('assessments',aid,sid,name,kind,weight,date.isoformat())
                assessment_ids.append((aid,date))
            return sid,lead,start,day,assessment_ids

        def enroll(student, info, term, drop=False):
            nonlocal eid,gid,attendance_id
            section_id,lead,start,day,assessments = info
            eid+=1
            dropped = ('2026-03-06' if term==1 else '2026-09-12') if drop else None
            if dropped and dropped>as_of: dropped=None
            enrolled = '2026-02-25' if term==1 else '2026-09-02'
            put('enrollments',eid,student,section_id,'dropped' if dropped else 'completed' if term==1 else 'enrolled',enrolled,dropped)
            if not dropped:
                scores=[]
                for assessment,date in assessments:
                    gid+=1
                    ready=date<=snapshot
                    absent=ready and rng.random()<.012
                    score=0 if absent else max(30,min(100,round(rng.gauss(abilities[student],7)))) if ready else None
                    put('grades',gid,eid,assessment,score,'absent' if absent else 'graded' if ready else 'pending',lead if ready else None,date.isoformat() if ready else None)
                    scores.append(score)
                if term==1 and sum(s*w for s,w in zip(scores,[30,20,50]))>=6000:
                    course=db.execute('SELECT course_id FROM sections WHERE id=?',(section_id,)).fetchone()[0]
                    passed.add((student,course))
            for week in range(16):
                date=start+dt.timedelta(weeks=week,days=day-1)
                if date>snapshot or dropped and date.isoformat()>=dropped: continue
                if not db.execute('SELECT is_teaching_day FROM calendar_days WHERE term_id=? AND calendar_date=?',(term,date.isoformat())).fetchone()[0]: continue
                attendance_id+=1
                state=rng.choices(['present','late','excused','absent'],[92,3,3,2])[0]
                put('attendance',attendance_id,eid,section_id,date.isoformat(),state,lead)

        # All administrative classes have distinct slots per student. Repeats use spare slots.
        for term in (1,2):
            for cid in range(1,41):
                admission=2023+(cid-1)%4
                year=2026-admission+(1 if term==2 else 0)
                if not 1<=year<=4: continue
                school=(cid-1)//8+1
                core=[(school-1)*20+(year-1)*4+j+1 for j in range(4)]
                elective=(school-1)*20+17+(year-1)%4
                other=school%5+1
                cross=(other-1)*20+17+(year-1)%4
                used_slots=[]
                for j,course in enumerate(core+[elective,cross]):
                    slot=((cid-1)%5+j)%10
                    used_slots.append(slot)
                    info=section(term,course,year,slot,25 if cid%9==0 and j==0 else 32)
                    prereq=db.execute('SELECT required_course_id FROM prerequisites WHERE course_id=?',(course,)).fetchone()
                    for (student,) in students[cid]:
                        if prereq and (student,prereq[0]) not in passed: continue
                        enroll(student,info,term,drop=j>=4 and student%97==0)
                if term==2 and year>1:
                    free=[slot for slot in range(10) if slot not in used_slots]
                    previous=[c-4 for c in core]
                    for course,slot in zip(previous,free):
                        repeats=[s for (s,) in students[cid] if (s,course) not in passed]
                        if repeats:
                            info=section(term,course,year-1,slot)
                            for student in repeats: enroll(student,info,term)
            # A few genuinely empty electives, including cancelled classes.
            for school in range(1,6):
                section(term,(school-1)*20+20,1,9,20,cancelled=school==5)
        # Publish all completed spring assessments and a subset of current results.
        for assessment,term,date,lead in db.execute('SELECT a.id,s.term_id,a.assessment_date,t.teacher_id FROM assessments a JOIN sections s ON s.id=a.section_id JOIN section_teachers t ON t.section_id=s.id AND t.is_lead=1 WHERE s.status<>\'cancelled\' ORDER BY a.id').fetchall():
            if date<=as_of and (term==1 or assessment%4!=0):
                put('grade_releases',assessment,lead,date+'T18:00:00+08:00')
        for table,label in TABLE_LABELS.items():
            put('schema_comments',table,'',label)
            for col in db.execute(f'PRAGMA table_info({table})').fetchall():
                put('schema_comments',table,col[1],COL_LABELS[col[1]])
        checks=validate(db)
        db.execute('ANALYZE')
        db.commit()
        complete=True
        return {'database':str(path),'seed':seed,'as_of':as_of,'bytes':path.stat().st_size,
                'invariant_checks':checks,'rows':{t:db.execute(f'SELECT COUNT(*) FROM {t}').fetchone()[0] for t in TABLE_LABELS}}
    finally:
        if db is not None: db.close()
        if not complete: path.unlink(missing_ok=True)


if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--db',default=str(ROOT/'data/academic_demo.sqlite3'))
    parser.add_argument('--seed',type=int,default=20261113)
    parser.add_argument('--as-of',default='2026-11-13',help='Snapshot date: 2026-09-02 through 2026-12-27')
    args=parser.parse_args()
    print(json.dumps(generate(args.db,args.seed,args.as_of),ensure_ascii=False,indent=2))
