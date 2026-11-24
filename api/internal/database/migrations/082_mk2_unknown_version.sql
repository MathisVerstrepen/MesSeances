-- MK2 can publish a projection format without a language or version.
DO $$
DECLARE
    target_table text;
    constraint_name text;
    previous_expression text;
BEGIN
    FOREACH target_table IN ARRAY ARRAY['showtimes', 'screening_history_showtimes'] LOOP
        FOREACH constraint_name IN ARRAY ARRAY['showtimes_language_check', 'showtimes_provider_version_check'] LOOP
            SELECT pg_get_expr(conbin, conrelid) INTO STRICT previous_expression
            FROM pg_constraint
            WHERE conrelid = target_table::regclass AND conname = constraint_name AND contype = 'c';
            EXECUTE format('ALTER TABLE %I DROP CONSTRAINT %I', target_table, constraint_name);
            EXECUTE format(
                'ALTER TABLE %I ADD CONSTRAINT %I CHECK ((%s) OR (provider = ''mk2'' AND language = '''' AND provider_version = ''''))',
                target_table, constraint_name, previous_expression);
        END LOOP;
    END LOOP;
END $$;
