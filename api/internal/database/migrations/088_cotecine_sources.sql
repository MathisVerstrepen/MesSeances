-- Cotecine (Cinegroup): independent cinemas whose Cinegroup site, sold
-- through Cotecine, lists the whole programme on one "seances/" page. The
-- generic provider is registered here with the cinemas missing from
-- MesSeances that migrations 085 to 087 do not already seed. Each source key
-- is the Cinegroup theater id (the page's dataLayer theaterID); website is the
-- site base URL, a sub-path when one site hosts several cinemas; cnc_id is the
-- cinema's CNC establishment number; params holds the cinema address
-- (address, postal_code, city), which the programme page does not reliably
-- show. Rows are inserted disabled.

INSERT INTO providers (key) VALUES ('cotecine');

INSERT INTO sync_sources (provider, source_key, name, website, cnc_id, params)
VALUES
    ('cotecine', '208', 'Pierre Brasseur (Vitry-le-François)', 'https://www.cine-vitry.fr/', '463842', '{"address": "18 Grande Rue de Vaux", "postal_code": "51300", "city": "Vitry-le-François"}'::jsonb),
    ('cotecine', '403', 'Gérard Philipe (Mers-les-Bains)', 'https://www.cinemagerardphilipe.fr/', '258652', '{"address": "Rue Paul Doumer", "postal_code": "80350", "city": "Mers-les-Bains"}'::jsonb),
    ('cotecine', '496', 'Le Challenger (Challes-les-Eaux)', 'https://www.cinemachallenger.fr/', '94802', '{"address": "Place Louis Dumollard", "postal_code": "73190", "city": "Challes-les-Eaux"}'::jsonb),
    ('cotecine', '563', 'La Bobine (Bréal-sous-Montfort)', 'https://www.cinelabobine35.fr/', '71942', '{"address": "30 rue de la Petite Motte", "postal_code": "35310", "city": "Bréal-sous-Montfort"}'::jsonb),
    ('cotecine', '571', 'Le Bretagne (Guichen)', 'https://www.cinebretagne.fr/', '188041', '{"address": "Péguin", "postal_code": "35580", "city": "Guichen"}'::jsonb),
    ('cotecine', '609', 'L''Eden (Romilly-sur-Seine)', 'https://www.cinemaedenromilly.fr/', '342071', '{"address": "66 rue Gambetta", "postal_code": "10100", "city": "Romilly-sur-Seine"}'::jsonb),
    ('cotecine', '649', 'Ciné Mérignac (Mérignac)', 'https://www.cinemerignac.fr/', '258161', '{"address": "Place Charles de Gaulle", "postal_code": "33700", "city": "Mérignac"}'::jsonb),
    ('cotecine', '715', 'Grand Écran (Mauges-sur-Loire)', 'https://www.cinemagrandecran.org/', '317911', '{"address": "Rue de la Loire", "postal_code": "49620", "city": "Mauges-sur-Loire"}'::jsonb),
    ('cotecine', '831', 'Espace Méliès (Lure)', 'https://www.cinemaespacemelies.fr/', '236833', '{"address": "Esplanade Charles de Gaulle", "postal_code": "70200", "city": "Lure"}'::jsonb),
    ('cotecine', '1731', 'L''Archipel (Paris)', 'https://www.larchipelcinema.com/', '1392', '{"address": "17 boulevard de Strasbourg", "postal_code": "75010", "city": "Paris"}'::jsonb),
    ('cotecine', '1824', 'Cinéma Confluences (Varennes-sur-Seine)', 'https://www.cinema-confluences.com/varennes/', '442370', '{"address": "Espace commercial du Bréau - RD606", "postal_code": "77130", "city": "Varennes-sur-Seine"}'::jsonb),
    ('cotecine', '1834', 'Cinéma Jean Gabin (Champagne-sur-Seine)', 'https://www.cinema-confluences.com/champagne/', '96533', '{"address": "Rue Pierre de Coubertin", "postal_code": "77430", "city": "Champagne-sur-Seine"}'::jsonb),
    ('cotecine', '1844', 'Cinéma Confluences (Sens)', 'https://www.cinema-confluences.com/sens/', '359772', '{"address": "Zone commerciale Champbertrand", "postal_code": "89100", "city": "Sens"}'::jsonb),
    ('cotecine', '3034', 'Cinéma Confluences (Bar-le-Duc)', 'https://www.cinema-confluences.com/bar-le-duc/', '39487', '{"address": "8 avenue du 94e Régiment d''Infanterie", "postal_code": "55000", "city": "Bar-le-Duc"}'::jsonb),
    ('cotecine', '3851', 'Le Trianon (Valognes)', 'https://www.cine-valognes.fr/', '441261', '{"address": "4 rue du Palais de Justice", "postal_code": "50700", "city": "Valognes"}'::jsonb),
    ('cotecine', '3939', 'Cinéma Le César (Apt)', 'https://www.cinemalecesar.fr/', '21532', '{"address": "Rue Scudéry", "postal_code": "84400", "city": "Apt"}'::jsonb),
    ('cotecine', '4013', 'Olympia (Cannes)', 'https://www.cinemaolympia-cannes.fr/', '84711', '{"address": "5 rue d''Antibes", "postal_code": "06400", "city": "Cannes"}'::jsonb),
    ('cotecine', '4173', 'L''Étoile (Carantec)', 'https://www.cinema-etoile-carantec.fr/', '85902', '{"address": "Place de l''Étoile", "postal_code": "29660", "city": "Carantec"}'::jsonb),
    ('cotecine', '4203', 'Le Lumière (L''Union)', 'https://www.cinelumiere-lunion.fr/', '437682', '{"address": "6 rue du Pic du Midi", "postal_code": "31240", "city": "L''Union"}'::jsonb),
    ('cotecine', '4257', 'Cinéma Confluences (Sablé-sur-Sarthe)', 'https://www.cinema-confluences.com/sable-sur-sarthe/', '348315', '{"address": "2 bis rue Saint-Denis", "postal_code": "72300", "city": "Sablé-sur-Sarthe"}'::jsonb),
    ('cotecine', '4408', 'La Balise (Douarnenez)', 'https://www.cinemalabalise.fr/', '141712', '{"address": "39 rue Louis Pasteur", "postal_code": "29100", "city": "Douarnenez"}'::jsonb),
    ('cotecine', '4458', 'Cinéma Confluences (Mennecy)', 'https://www.cinema-confluences.com/mennecy/', '256983', '{"address": "5 rue Charles Péguy", "postal_code": "91540", "city": "Mennecy"}'::jsonb),
    ('cotecine', '4462', 'L''Ellipse (Ajaccio)', 'https://www.cinema-ajaccio.fr/ellipse/', '13686', '{"address": "Boulevard Louis Campi", "postal_code": "20090", "city": "Ajaccio"}'::jsonb),
    ('cotecine', '4464', 'Laetitia (Ajaccio)', 'https://www.cinema-ajaccio.fr/laetitia/', '13693', '{"address": "24 cours Napoléon", "postal_code": "20000", "city": "Ajaccio"}'::jsonb)
ON CONFLICT (provider, source_key) DO NOTHING;
