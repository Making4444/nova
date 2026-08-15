#!/usr/bin/env python3
"""
WhatsApp Chat Exporter -> Nova JSONL Converter
==============================================
يقوم هذا السكريبت بقراءة ملف تصدير محادثة واتساب (.txt)
وتحويل الرسائل القديمة إلى صيغة JSONL المتوافقة تماماً مع بوت نوفا.
يحافظ على أي رسائل جديدة موجودة بالفعل في ملف JSONL دون مساس.
"""

import os
import re
import sys
import json
import uuid
import argparse
from datetime import datetime

# ضبط ترميز الطرفية على UTF-8
if sys.stdout and hasattr(sys.stdout, 'reconfigure'):
    sys.stdout.reconfigure(encoding='utf-8')
if sys.stderr and hasattr(sys.stderr, 'reconfigure'):
    sys.stderr.reconfigure(encoding='utf-8')

# جدول تحويل الأرقام العربية/الهندية إلى أرقام إنجليزية
ARABIC_DIGITS_TRANS = str.maketrans('٠١٢٣٤٥٦٧٨٩', '0123456789')

# أحرف التحكم الخفية في اتجاه النصوص في واتساب
RTL_LTR_CONTROL_CHARS = re.compile(r'[\u200e\u200f\u202a-\u202e\u2066-\u2069\ufeff\u200b]')

def clean_invisible_chars(s: str) -> str:
    """إزالة أحرف التوجيه الخفية والعلامات غير المرئية."""
    if not s:
        return ""
    return RTL_LTR_CONTROL_CHARS.sub('', s).strip()

def normalize_digits(s: str) -> str:
    """تحويل الأرقام العربية إلى إنجليزية."""
    return s.translate(ARABIC_DIGITS_TRANS)

def parse_arabic_timestamp(date_str: str, time_str: str, am_pm_str: str) -> str:
    """تحويل التاريخ والوقت العربي إلى ISO 8601 UTC/Local timestamp."""
    clean_date = normalize_digits(clean_invisible_chars(date_str))
    clean_time = normalize_digits(clean_invisible_chars(time_str))
    clean_ampm = clean_invisible_chars(am_pm_str)

    # Format usually: D/M/YYYY
    parts = clean_date.split('/')
    if len(parts) != 3:
        return datetime.now().isoformat()

    day, month, year = int(parts[0]), int(parts[1]), int(parts[2])
    time_parts = clean_time.split(':')
    hour, minute = int(time_parts[0]), int(time_parts[1])

    # ص (AM) أو م (PM)
    if 'م' in clean_ampm or 'PM' in clean_ampm.upper():
        if hour < 12:
            hour += 12
    elif 'ص' in clean_ampm or 'AM' in clean_ampm.upper():
        if hour == 12:
            hour = 0

    try:
        dt = datetime(year, month, day, hour, minute)
        return dt.isoformat() + "Z"
    except Exception:
        return datetime.now().isoformat() + "Z"

def parse_whatsapp_chat(file_path: str, cutoff_trigger_old_bot: bool = True):
    """تحليل ملف نص واتساب واستخراج قائمة الرسائل."""
    with open(file_path, 'r', encoding='utf-8') as f:
        lines = f.readlines()

    # نمط بداية رسالة واتساب
    # مثال: ١‏/٨‏/٢٠٢٦، ٩:٤٤ م - Name: Text
    msg_header_pattern = re.compile(
        r'^([٠-٩0-9]+/[٠-٩0-9]+/[٠-٩0-9]+)[،,]\s*([٠-٩0-9]+:[٠-٩0-9]+)\s*(ص|م|AM|PM|am|pm)\s*-\s*(.*?)$'
    )

    messages = []
    current_msg = None

    for line_idx, raw_line in enumerate(lines, start=1):
        line = raw_line.rstrip('\r\n')
        clean_line = clean_invisible_chars(line)
        
        if not clean_line:
            if current_msg:
                current_msg['text'] += '\n'
            continue

        match = msg_header_pattern.match(clean_line)
        if match:
            date_str, time_str, am_pm_str, content = match.groups()
            content = content.strip()

            # التحقق هل الرسالة تحتوي على اسم مرسل (sender: text)
            if ':' in content:
                sender_part, text_part = content.split(':', 1)
                sender_name = clean_invisible_chars(sender_part).strip('~ ').strip()
                message_text = text_part.strip()

                # استبدال وسائط واتساب المستبعدة بوسم لطيف
                if '<تم استبعاد الوسائط>' in message_text or '<Media omitted>' in message_text:
                    message_text = '[Media]'
                elif 'تم حذف هذه الرسالة' in message_text:
                    message_text = '[Deleted Message]'

                # لو وصلنا لجزء التجارب القديمة للبوت عند تريجر "يا نوفا انتا فاكر حد هنا" وحابب تستثني التجارب القديمة
                if cutoff_trigger_old_bot and 'يا نوفا انتا فاكر حد هنا' in message_text:
                    # نتوقف قبل بدء تجارب البوت القديم ذو الردود المكررة
                    print(f"[*] تم التوقف عند السطر {line_idx} (بدء تجارب نوفا القديمة) للحفاظ على نقاء الذاكرة.")
                    break

                ts = parse_arabic_timestamp(date_str, time_str, am_pm_str)
                msg_id = f"HIST_{uuid.uuid4().hex[:12].upper()}"

                is_nova = (sender_name.lower() == 'nova')

                current_msg = {
                    "message_id": msg_id,
                    "sender_id": f"{sender_name.replace(' ', '_')}@whatsapp.user",
                    "sender_name": sender_name,
                    "text": message_text,
                    "is_nova": is_nova,
                    "timestamp": ts
                }
                messages.append(current_msg)
            else:
                # رسالة نظام (مثل: أنت أنشأت المجموعة / تمت إضافة فلان)
                current_msg = None
        else:
            # تكملة للرسالة السابقة (Multi-line message)
            if current_msg:
                current_msg['text'] += '\n' + line

    return messages

def import_to_jsonl(txt_path: str, output_jsonl_path: str, cutoff_old_tests: bool = True):
    """استيراد المحادثة ودمجها بملف الـ JSONL دون مساس بالرسائل الجديدة المحسنة."""
    print(f"[*] جاري قراءة وتحليل ملف: {txt_path}")
    imported_msgs = parse_whatsapp_chat(txt_path, cutoff_trigger_old_bot=cutoff_old_tests)
    print(f"[+] تم استخراج {len(imported_msgs)} رسالة من السجل القديم بنجاح.")

    existing_msgs = []
    existing_texts = set()

    # لو ملف JSONL موجود بالفعل (فيه رسائل جديدة مسجلة من البوت الجديد)
    if os.path.exists(output_jsonl_path):
        print(f"[*] تم العثور على ملف محادثة حالي: {output_jsonl_path}")
        with open(output_jsonl_path, 'r', encoding='utf-8') as f:
            for line in f:
                line_str = line.strip()
                if line_str:
                    try:
                        m = json.loads(line_str)
                        existing_msgs.append(m)
                        # مفتاح لتمييز الرسالة ومنع التكرار
                        existing_texts.add((m.get('sender_name', ''), m.get('text', '').strip()))
                    except Exception:
                        pass
        print(f"[+] الملف الحالي يحتوي على {len(existing_msgs)} رسالة جديدة (لن يتم المساس بها).")

    # تصفية الرسائل القديمة لمنع تكرار أي رسالة موجودة
    filtered_imported = []
    for m in imported_msgs:
        key = (m.get('sender_name', ''), m.get('text', '').strip())
        if key not in existing_texts:
            filtered_imported.append(m)

    # الدمج: الرسائل القديمة تسبق الرسائل الجديدة المسجلة حديثاً
    final_messages = filtered_imported + existing_msgs

    # التأكد من وجود المجلد
    os.makedirs(os.path.dirname(os.path.abspath(output_jsonl_path)), exist_ok=True)

    with open(output_jsonl_path, 'w', encoding='utf-8') as f:
        for m in final_messages:
            f.write(json.dumps(m, ensure_ascii=False) + '\n')

    print(f"\n🎉 تم بنجاح حفظ {len(final_messages)} رسالة بالكامل في:\n   -> {output_jsonl_path}")
    print("✨ الرسائل القديمة تم دمجها، والرسائل الجديدة المحسنة ظلت محفوظة 100%!")

if __name__ == '__main__':
    parser = argparse.ArgumentParser(description="تحويل سجل دردشة واتساب إلى JSONL لبوت نوفا")
    parser.add_argument('--input', '-i', default="files/‏دردشة في واتساب مع جروب هنسميه بعدين.txt", help="مسار ملف الدردشة txt")
    parser.add_argument('--output', '-o', default="", help="مسار ملف JSONL المستهدف (لو ترك فارغاً سيبحث تلقائياً عن جروب واتساب الفعلي)")
    parser.add_argument('--include-all', action='store_true', help="تضمين حتى الرسائل التجريبية القديمة لبوت نوفا")

    args = parser.parse_args()

    input_file = args.input
    if not os.path.exists(input_file):
        # البحث عن أي ملف .txt في مجلد files
        files_dir = "files"
        if os.path.exists(files_dir):
            for fname in os.listdir(files_dir):
                if fname.endswith(".txt"):
                    input_file = os.path.join(files_dir, fname)
                    break

    output_file = args.output
    if not output_file:
        # البحث التلقائي عن أي ملف جروب واتساب فعلي موجود في data/chats/groups
        groups_dir = os.path.join("data", "chats", "groups")
        if os.path.exists(groups_dir):
            group_files = [f for f in os.listdir(groups_dir) if f.endswith("@g.us.jsonl")]
            if group_files:
                output_file = os.path.join(groups_dir, group_files[0])
                print(f"[🔍] تم اكتشاف ملف الجروب الفعلي تلقائياً: {output_file}")

        if not output_file:
            output_file = os.path.join("data", "chats", "groups", "default_group.jsonl")

    import_to_jsonl(input_file, output_file, cutoff_old_tests=(not args.include_all))
