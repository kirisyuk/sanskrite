import csv
import re

input_file = 'raw_data.csv'
output_file = 'cleaned_data.csv'

# Mapping for column renaming
column_mapping = {
    'id': 'id',
    'contract_id': 'contract_id',
    'Отправитель': 'sender',
    'Получатель': 'receiver',
    'Номер Документа': 'doc_number',
    'category_name': 'category',
    'Товар': 'product',
    'Сумма good_value': 'volume',
    'Еденица измерения': 'unit',
    'Сумма Накладных': 'amount',
    'Дата Поставки': 'delivery_date',
    'Сроки поставок': 'delivery_period',
    'СтатусПросрочки': 'status_overdue',
    'Срок окончания поставок по допику': 'deadline_additional'
}

def clean_amount(val):
    if not val:
        return None
    # Remove '₸', spaces and convert to float
    clean = re.sub(r'[^\d\.]', '', val)
    try:
        return float(clean)
    except ValueError:
        return None

with open(input_file, mode='r', encoding='utf-8') as infile, \
     open(output_file, mode='w', encoding='utf-8', newline='') as outfile:
    
    reader = csv.DictReader(infile)
    
    # Create new header based on mapping
    fieldnames = [column_mapping.get(col, col) for col in reader.fieldnames]
    writer = csv.DictWriter(outfile, fieldnames=fieldnames)
    
    writer.writeheader()
    
    for row in reader:
        new_row = {}
        for col, val in row.items():
            new_col = column_mapping.get(col, col)
            
            # Specific cleaning for 'amount'
            if new_col == 'amount':
                new_row[new_col] = clean_amount(val)
            else:
                new_row[new_col] = val
        
        writer.writerow(new_row)

print(f"Successfully processed {input_file} to {output_file}")




